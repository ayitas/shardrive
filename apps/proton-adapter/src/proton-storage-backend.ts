import { ProtonAuthRequiredError } from './proton-client-factory.js';
import { StorageBackendError, type AccountRequest, type ObjectInfo, type ObjectRequest, type StorageBackend, type StorageUsage, type UploadObject } from './storage-backend.js';

export interface ProtonNode {
	uid: string;
	type?: string;
	modificationTime?: Date;
	activeRevision?: { claimedSize?: number; storageSize?: number };
}

export interface ProtonUploadController {
	completion(): Promise<{ nodeUid: string; nodeRevisionUid: string }>;
}

export interface ProtonFileUploader {
	uploadFromStream(stream: ReadableStream<Uint8Array>, thumbnails: never[]): Promise<ProtonUploadController>;
}

export interface ProtonDownloadController {
	completion(): Promise<void>;
}

export interface ProtonFileDownloader {
	getClaimedSizeInBytes(): number | undefined;
	downloadToStream(stream: WritableStream<Uint8Array>): ProtonDownloadController;
}

export interface ProtonDriveClientLike {
	getMyFilesRootFolder(): Promise<ProtonNode>;
	getFileUploader(parentFolderUid: string, name: string, metadata: { mediaType: string; expectedSize: number }): Promise<ProtonFileUploader>;
	getFileDownloader(nodeUid: string): Promise<ProtonFileDownloader>;
	getNode(nodeUid: string): Promise<ProtonNode>;
	trashNodes(nodeUids: string[]): AsyncGenerator<{ uid: string; ok: true } | { uid: string; ok: false; error: Error }>;
	deleteNodes(nodeUids: string[]): AsyncGenerator<{ uid: string; ok: true } | { uid: string; ok: false; error: Error }>;
}

export interface ProtonAccountRuntime {
	client: ProtonDriveClientLike;
	usage(): Promise<StorageUsage>;
	health(): Promise<void>;
}

export type ProtonAccountRuntimeFactory = (accountId: string) => Promise<ProtonAccountRuntime>;

/**
 * Maps the official SDK's node/file streams to Shardrive's object-store
 * contract. It intentionally knows nothing about the SDK package or session
 * storage; those are supplied by the runtime factory.
 */
export class ProtonStorageBackend implements StorageBackend {
	constructor(private readonly runtimeFor: ProtonAccountRuntimeFactory) {}

	async upload(input: UploadObject): Promise<{ objectId: string; sizeBytes: number }> {
		try {
			debugRuntime('upload.start', input.accountId, input.sizeBytes);
			const runtime = await this.runtimeFor(input.accountId);
			const root = await runtime.client.getMyFilesRootFolder();
			debugRuntime('upload.root', input.accountId);
			const uploader = await runtime.client.getFileUploader(root.uid, input.objectId, {
				mediaType: 'application/octet-stream',
				expectedSize: input.sizeBytes
			});
			debugRuntime('upload.uploader', input.accountId);
			const controller = await uploader.uploadFromStream(readableFromChunks(input.chunks), []);
			debugRuntime('upload.stream-started', input.accountId);
			const result = await controller.completion();
			debugRuntime('upload.completed', input.accountId);
			if (!result.nodeUid) throw new Error('Proton upload returned no node UID');
			return { objectId: result.nodeUid, sizeBytes: input.sizeBytes };
		} catch (error) {
			throw normalizeError('upload', error);
		}
	}

	async *download(input: ObjectRequest): AsyncIterable<Buffer> {
		const queue = new ByteQueue();
		try {
			const runtime = await this.runtimeFor(input.accountId);
			const downloader = await runtime.client.getFileDownloader(input.objectId);
			const controller = downloader.downloadToStream(new WritableStream<Uint8Array>({
				write: (chunk) => queue.push(Buffer.from(chunk)),
				close: () => queue.close(),
				abort: (reason) => queue.fail(reason)
			}));
			void controller.completion().then(() => queue.close(), (error) => queue.fail(error));
			for await (const chunk of queue) yield chunk;
		} catch (error) {
			queue.fail(error);
			throw normalizeError('download', error);
		}
	}

	async delete(input: ObjectRequest): Promise<void> {
		try {
			const client = (await this.runtimeFor(input.accountId)).client;
			await assertDeleteResult(client.trashNodes([input.objectId]));
			await assertDeleteResult(client.deleteNodes([input.objectId]));
		} catch (error) {
			throw normalizeError('delete', error);
		}
	}

	async stat(input: ObjectRequest): Promise<ObjectInfo> {
		try {
			const node = await (await this.runtimeFor(input.accountId)).client.getNode(input.objectId);
			const sizeBytes = node.activeRevision?.claimedSize;
			if (sizeBytes === undefined) throw new Error('Proton node has no cleartext claimed size');
			return { objectId: node.uid, sizeBytes, modifiedAt: node.modificationTime ?? new Date(0) };
		} catch (error) {
			throw normalizeError('stat', error);
		}
	}

	async usage(input: AccountRequest): Promise<StorageUsage> {
		try {
			return await (await this.runtimeFor(input.accountId)).usage();
		} catch (error) {
			throw normalizeError('usage', error);
		}
	}

	async health(input: AccountRequest): Promise<boolean> {
		try {
			await (await this.runtimeFor(input.accountId)).health();
			return true;
		} catch (error) {
			throw normalizeError('health', error);
		}
	}
}

function debugRuntime(operation: string, accountId: string, sizeBytes?: number): void {
	if (process.env.SHARDRIVE_PROTON_DEBUG_RUNTIME !== '1') return;
	console.error(JSON.stringify({ runtime: operation, accountId, ...(sizeBytes === undefined ? {} : { sizeBytes }) }));
}

function readableFromChunks(chunks: AsyncIterable<Buffer>): ReadableStream<Uint8Array> {
	const iterator = chunks[Symbol.asyncIterator]();
	return new ReadableStream<Uint8Array>({
		async pull(controller) {
			try {
				const next = await iterator.next();
				if (next.done) controller.close();
				else controller.enqueue(next.value);
			} catch (error) {
				controller.error(error);
			}
		},
		async cancel() {
			await iterator.return?.();
		}
	});
}

async function assertDeleteResult(results: AsyncGenerator<{ uid: string; ok: true } | { uid: string; ok: false; error: Error }>): Promise<void> {
	for await (const result of results) {
		if (!result.ok) throw result.error;
	}
}

class ByteQueue implements AsyncIterable<Buffer> {
	private values: Buffer[] = [];
	private waiters: Array<(result: IteratorResult<Buffer>) => void> = [];
	private ended = false;
	private failure: unknown;

	push(value: Buffer) {
		if (this.ended) return;
		const waiter = this.waiters.shift();
		if (waiter) waiter({ done: false, value });
		else this.values.push(value);
	}
	close() {
		if (this.ended) return;
		this.ended = true;
		for (const waiter of this.waiters.splice(0)) waiter({ done: true, value: undefined });
	}
	fail(error: unknown) {
		if (this.ended) return;
		this.failure = error;
		this.ended = true;
		for (const waiter of this.waiters.splice(0)) waiter({ done: true, value: undefined });
	}
	[Symbol.asyncIterator]() { return this; }
	next(): Promise<IteratorResult<Buffer>> {
		if (this.values.length > 0) return Promise.resolve({ done: false, value: this.values.shift()! });
		if (this.ended) return this.failure ? Promise.reject(this.failure) : Promise.resolve({ done: true, value: undefined });
		return new Promise((resolve) => this.waiters.push(resolve));
	}
}

function normalizeError(operation: string, error: unknown): StorageBackendError {
	if (error instanceof StorageBackendError) return error;
	if (error instanceof ProtonAuthRequiredError) return new StorageBackendError('UNAUTHENTICATED', `Proton ${operation} requires authentication`, { cause: error });
	if (isTimeoutError(error)) return new StorageBackendError('UNAVAILABLE', `Proton ${operation} timed out`, { cause: error });
	const status = typeof error === 'object' && error !== null ? Reflect.get(error, 'status') ?? Reflect.get(error, 'statusCode') : undefined;
	const code = status === 401 ? 'UNAUTHENTICATED' : status === 403 ? 'PERMISSION_DENIED' : status === 404 ? 'NOT_FOUND' : status === 429 ? 'RESOURCE_EXHAUSTED' : status === 503 ? 'UNAVAILABLE' : 'INTERNAL';
	return new StorageBackendError(code, `Proton ${operation} failed`, { cause: error });
}

function isTimeoutError(error: unknown): boolean {
	if (!(error instanceof Error)) return false;
	return error.name === 'TimeoutError' || error.name === 'AbortError' || /timed out|timeout/i.test(error.message);
}
