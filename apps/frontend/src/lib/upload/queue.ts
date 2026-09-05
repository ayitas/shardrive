import type {
	CompleteUploadResponse,
	CreateUploadRequest,
	ShardriveApi,
} from '$lib/api/client';

export type UploadState = 'QUEUED' | 'CREATING' | 'UPLOADING' | 'PAUSED' | 'VERIFYING' | 'COMPLETED' | 'CANCELLED' | 'FAILED';

export interface UploadSnapshot {
	state: UploadState;
	uploadId?: string;
	fileId?: string;
	completedChunks: number;
	completedIndexes: number[];
	totalChunks: number;
	chunkSize: number;
	retrying: boolean;
	retryAttempt: number;
	receivedBytes: number;
	totalBytes: number;
	speedBytesPerSecond: number;
	etaSeconds?: number;
	error?: string;
	result?: CompleteUploadResponse;
}

export interface UploadQueueOptions {
	concurrency?: number;
	maxRetries?: number;
	baseRetryDelayMs?: number;
	onChange?: (snapshot: UploadSnapshot) => void;
	resume?: { uploadId: string; fileId: string; chunkSize: number; chunkCount: number };
}

type UploadApi = Pick<ShardriveApi, 'createUpload' | 'getUpload' | 'uploadChunk' | 'completeUpload' | 'cancelUpload'>;

/** A bounded, resumable browser upload. It never reads the whole File into memory. */
export class UploadQueue {
	private readonly concurrency: number;
	private readonly onChange?: (snapshot: UploadSnapshot) => void;
	private readonly maxRetries: number;
	private readonly baseRetryDelayMs: number;
	private readonly resumeUpload?: UploadQueueOptions['resume'];
	private paused = false;
	private cancelled = false;
	private running = 0;
	private nextIndex = 0;
	private chunkSize = 0;
	private completed = new Set<number>();
	private snapshot: UploadSnapshot;
	private startedAt = 0;

	constructor(
		private readonly api: UploadApi,
		private readonly file: File,
		private readonly request: CreateUploadRequest,
		options: UploadQueueOptions = {}
	) {
		this.concurrency = Math.max(1, Math.floor(options.concurrency ?? 3));
		this.onChange = options.onChange;
		this.maxRetries = Math.max(0, Math.floor(options.maxRetries ?? 5));
		this.baseRetryDelayMs = Math.max(1, options.baseRetryDelayMs ?? 250);
		this.resumeUpload = options.resume;
		this.snapshot = {
			state: 'QUEUED', completedChunks: 0, completedIndexes: [], totalChunks: 0, chunkSize: 0, retrying: false, retryAttempt: 0, receivedBytes: 0, totalBytes: file.size, speedBytesPerSecond: 0
		};
	}

	getSnapshot(): UploadSnapshot { return { ...this.snapshot }; }

	pause(): void {
		if (this.snapshot.state === 'UPLOADING' || this.snapshot.state === 'CREATING') {
			this.paused = true;
			this.setState('PAUSED');
		}
	}

	resume(): void {
		if (this.snapshot.state !== 'PAUSED') return;
		this.paused = false;
		this.setState('UPLOADING');
		this.schedule();
	}

	async cancel(): Promise<void> {
		if (!this.snapshot.uploadId || this.snapshot.state === 'COMPLETED' || this.snapshot.state === 'CANCELLED' || this.snapshot.state === 'FAILED') return;
		this.cancelled = true;
		this.paused = true;
		try {
			await this.api.cancelUpload(this.snapshot.uploadId);
			this.setState('CANCELLED');
			this.reject?.(new Error('Upload cancelled'));
		} catch (error) {
			this.cancelled = false;
			this.paused = false;
			throw error;
		}
	}

	async start(): Promise<CompleteUploadResponse> {
		if (this.snapshot.state !== 'QUEUED') throw new Error('upload has already started');
		this.setState(this.resumeUpload ? 'UPLOADING' : 'CREATING');
		this.startedAt = Date.now();
		try {
			const created = this.resumeUpload ?? await this.api.createUpload(this.request);
			this.chunkSize = created.chunkSize;
			this.snapshot = { ...this.snapshot, uploadId: created.uploadId, fileId: created.fileId, totalChunks: created.chunkCount, chunkSize: created.chunkSize };
			const status = await this.api.getUpload(created.uploadId);
			this.completed = new Set(status.completedIndexes);
			this.snapshot = { ...this.snapshot, completedChunks: this.completed.size, receivedBytes: status.receivedBytes };
			this.nextIndex = 0;
			this.setState(this.paused ? 'PAUSED' : 'UPLOADING');
			await new Promise<void>((resolve, reject) => {
				this.resolve = resolve;
				this.reject = reject;
				this.schedule();
			});
			this.setState('VERIFYING');
			const result = await this.api.completeUpload(created.uploadId);
			this.snapshot = { ...this.snapshot, state: 'COMPLETED', result };
			this.emit();
			return result;
		} catch (error) {
			const message = error instanceof Error ? error.message : 'Upload failed';
			this.snapshot = { ...this.snapshot, state: this.cancelled ? 'CANCELLED' : 'FAILED', error: this.cancelled ? undefined : message };
			this.emit();
			throw error;
		}
	}

	private resolve?: () => void;
	private reject?: (error: unknown) => void;

	private schedule(): void {
		if (this.paused || this.cancelled) return;
		while (this.running < this.concurrency && this.nextIndex < this.snapshot.totalChunks) {
			const index = this.nextIndex++;
			if (this.completed.has(index)) continue;
			this.running++;
			void this.uploadOne(index);
		}
		if (this.running === 0 && this.nextIndex >= this.snapshot.totalChunks) this.resolve?.();
	}

	private async uploadOne(index: number): Promise<void> {
		let lastError: unknown;
		for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
			if (this.cancelled) return;
			try {
			const start = index * this.chunkSize;
			const end = Math.min(this.file.size, start + this.chunkSize);
			await this.api.uploadChunk(this.snapshot.uploadId!, index, this.file.slice(start, end));
			this.completed.add(index);
			const receivedBytes = this.snapshot.receivedBytes + (end - start);
			const elapsedSeconds = Math.max((Date.now() - this.startedAt) / 1000, 0.001);
			const speedBytesPerSecond = receivedBytes / elapsedSeconds;
			this.snapshot = { ...this.snapshot, retrying: false, retryAttempt: 0, completedChunks: this.completed.size, completedIndexes: [...this.completed].sort((a, b) => a - b), receivedBytes, speedBytesPerSecond, etaSeconds: speedBytesPerSecond > 0 ? Math.ceil((this.snapshot.totalBytes - receivedBytes) / speedBytesPerSecond) : undefined };
			this.emit();
			return;
			} catch (error) {
				lastError = error;
			if (attempt < this.maxRetries) {
				this.snapshot = { ...this.snapshot, retrying: true, retryAttempt: attempt + 1 };
				this.emit();
				const jitteredDelay = this.baseRetryDelayMs * 2 ** attempt * (0.75 + Math.random() * 0.5);
					await new Promise((resolve) => setTimeout(resolve, jitteredDelay));
				}
			}
		}
		try {
			throw lastError;
		} catch (error) {
			this.reject?.(error);
		} finally {
			this.running--;
			this.schedule();
		}
	}

	private setState(state: UploadState): void { this.snapshot = { ...this.snapshot, state }; this.emit(); }
	private emit(): void { this.onChange?.(this.getSnapshot()); }
}
