import { env } from '$env/dynamic/public';

export interface ApiErrorBody {
	error?: {
		code?: string;
		message?: string;
	};
}

export class ApiError extends Error {
	readonly status: number;
	readonly code: string;

	constructor(status: number, code: string, message: string) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.code = code;
	}
}

export interface CreateUploadRequest {
	name: string;
	size: number;
	mimeType?: string;
	directoryId?: string;
}

export interface CreateUploadResponse {
	uploadId: string;
	fileId: string;
	chunkSize: number;
	chunkCount: number;
}

export interface UploadStatusResponse {
	uploadId: string;
	fileId: string;
	state: string;
	expectedSize: number;
	receivedBytes: number;
	expectedChunks: number;
	completedChunks: number;
	completedIndexes: number[];
	expiresAt: string;
}

export interface CompleteUploadResponse {
	fileId: string;
	state: string;
	checksum: string;
}

export interface ChunkResponse {
	index: number;
	state: string;
	bytes: number;
	checksum: string;
}

export interface FileSummary {
	id: string;
	directoryId: string | null;
	name: string;
	mimeType: string;
	sizeBytes: number;
	state: string;
	chunkCount: number;
	checksum: string | null;
	createdAt: string;
	updatedAt: string;
}

export interface DirectorySummary {
	id: string;
	userId: string;
	parentId: string | null;
	name: string;
	createdAt: string;
	updatedAt: string;
}
export interface AccountSummary { id: string; name: string; provider: string; state: string; totalBytes: number; usedBytes: number; freeBytes: number }

export interface AuthUser { userId: string; email?: string }

export class ShardriveApi {
	private readonly baseUrl: string;

	constructor(baseUrl = env.PUBLIC_API_BASE_URL ?? '') {
		this.baseUrl = baseUrl.replace(/\/$/, '');
	}

	async createUpload(request: CreateUploadRequest): Promise<CreateUploadResponse> {
		return this.request<CreateUploadResponse>('/api/v1/uploads', {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify(request)
		});
	}

	getUpload(uploadId: string): Promise<UploadStatusResponse> {
		return this.request<UploadStatusResponse>(`/api/v1/uploads/${encodeURIComponent(uploadId)}`);
	}

	getCompletedChunks(uploadId: string): Promise<Pick<UploadStatusResponse, 'uploadId' | 'completedIndexes'>> {
		return this.request(`/api/v1/uploads/${encodeURIComponent(uploadId)}/chunks`);
	}

	async uploadChunk(uploadId: string, index: number, body: Blob): Promise<ChunkResponse> {
		return this.request<ChunkResponse>(
			`/api/v1/uploads/${encodeURIComponent(uploadId)}/chunks/${index}`,
			{ method: 'PUT', headers: { 'content-type': 'application/octet-stream' }, body }
		);
	}

	completeUpload(uploadId: string): Promise<CompleteUploadResponse> {
		return this.request<CompleteUploadResponse>(`/api/v1/uploads/${encodeURIComponent(uploadId)}/complete`, {
			method: 'POST'
		});
	}

	cancelUpload(uploadId: string): Promise<void> {
		return this.request<void>(`/api/v1/uploads/${encodeURIComponent(uploadId)}`, { method: 'DELETE' });
	}

	listFiles(): Promise<{ files: FileSummary[] }> {
		return this.request('/api/v1/files');
	}

	listDirectories(parentId?: string): Promise<{ directories: DirectorySummary[] }> {
		const query = parentId ? `?parentId=${encodeURIComponent(parentId)}` : '';
		return this.request(`/api/v1/directories${query}`);
	}

	createDirectory(name: string, parentId?: string): Promise<DirectorySummary> {
		return this.request<DirectorySummary>('/api/v1/directories', {
			method: 'POST',
			headers: { 'content-type': 'application/json' },
			body: JSON.stringify({ name, parentId })
		});
	}

	renameFile(fileId: string, name: string): Promise<Pick<FileSummary, 'id' | 'name' | 'updatedAt'>> {
		return this.request(`/api/v1/files/${encodeURIComponent(fileId)}`, {
			method: 'PATCH', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ name })
		});
	}

	renameDirectory(directoryId: string, name: string): Promise<DirectorySummary> {
		return this.request(`/api/v1/directories/${encodeURIComponent(directoryId)}`, {
			method: 'PATCH', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ name })
		});
	}

	deleteDirectory(directoryId: string): Promise<void> {
		return this.request<void>(`/api/v1/directories/${encodeURIComponent(directoryId)}`, { method: 'DELETE' });
	}

	listAccounts(): Promise<{ accounts: AccountSummary[] }> { return this.request('/api/v1/accounts'); }
	deleteFile(fileId: string): Promise<void> { return this.request<void>(`/api/v1/files/${encodeURIComponent(fileId)}`, { method: 'DELETE' }); }

	login(email: string, password: string): Promise<AuthUser> {
		return this.request('/api/v1/auth/login', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ email, password }) });
	}

	logout(): Promise<void> { return this.request('/api/v1/auth/logout', { method: 'POST' }).then(() => undefined); }
	me(): Promise<AuthUser> { return this.request('/api/v1/auth/me'); }

	downloadUrl(fileId: string): string {
		return `${this.baseUrl}/api/v1/files/${encodeURIComponent(fileId)}/download`;
	}

	private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
		const response = await fetch(`${this.baseUrl}${path}`, {
			...init,
			credentials: 'include'
		});
		if (!response.ok) {
			let body: ApiErrorBody = {};
			try {
				body = (await response.json()) as ApiErrorBody;
			} catch {
				// Preserve the HTTP status when the server did not return JSON.
			}
			throw new ApiError(
				response.status,
				body.error?.code ?? 'http_error',
				body.error?.message ?? `Request failed with HTTP ${response.status}`
			);
		}
		if (response.status === 204 || response.status === 205) return undefined as T;
		const body = await response.text();
		if (!body.trim()) return undefined as T;
		return JSON.parse(body) as T;
	}
}
