export interface UploadObject {
	accountId: string;
	objectId: string;
	sizeBytes: number;
	chunks: AsyncIterable<Buffer>;
}

export interface ObjectRequest {
	accountId: string;
	objectId: string;
}

export interface AccountRequest {
	accountId: string;
}

export interface ObjectInfo {
	objectId: string;
	sizeBytes: number;
	modifiedAt: Date;
}

export interface StorageUsage {
	totalBytes: number;
	usedBytes: number;
	freeBytes: number;
}

export interface StorageBackend {
	upload(input: UploadObject): Promise<{ objectId: string; sizeBytes: number }>;
	download(input: ObjectRequest): AsyncIterable<Buffer>;
	delete(input: ObjectRequest): Promise<void>;
	stat(input: ObjectRequest): Promise<ObjectInfo>;
	usage(input: AccountRequest): Promise<StorageUsage>;
	health(input: AccountRequest): Promise<boolean>;
}

export type StorageErrorCode =
	| 'INVALID_ARGUMENT'
	| 'NOT_FOUND'
	| 'UNAUTHENTICATED'
	| 'PERMISSION_DENIED'
	| 'RESOURCE_EXHAUSTED'
	| 'UNAVAILABLE'
	| 'INTERNAL';

export class StorageBackendError extends Error {
	readonly code: StorageErrorCode;

	constructor(code: StorageErrorCode, message: string, options?: ErrorOptions) {
		super(message, options);
		this.name = 'StorageBackendError';
		this.code = code;
	}
}
