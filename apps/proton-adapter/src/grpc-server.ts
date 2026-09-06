import { once } from 'node:events';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import {
	StorageBackendError,
	type AccountRequest,
	type ObjectRequest,
	type StorageBackend,
	type StorageErrorCode,
	type UploadObject
} from './storage-backend.js';

const MAX_DATA_FRAME_BYTES = 4 * 1024 * 1024;
const PROTO_PATH = resolve(dirname(fileURLToPath(import.meta.url)), '../../../proto/storage.proto');
const require = createRequire(import.meta.url);

export async function loadStoragePackageDefinition(): Promise<protoLoader.PackageDefinition> {
	return protoLoader.load(PROTO_PATH, {
		keepCase: true,
		longs: Number,
		enums: String,
		defaults: true,
		oneofs: true,
		includeDirs: [dirname(requireGoogleProto('google/protobuf/empty.proto'))]
	});
}

export function createStorageAdapterServer(backend: StorageBackend): grpc.Server {
	const server = new grpc.Server();
	const packageDefinition = protoLoader.loadSync(PROTO_PATH, {
		keepCase: true,
		longs: Number,
		enums: String,
		defaults: true,
		oneofs: true,
		includeDirs: [dirname(requireGoogleProto('google/protobuf/empty.proto'))]
	});
	const loaded = grpc.loadPackageDefinition(packageDefinition) as unknown as {
		shardrive: { storage: { v1: { StorageAdapter: { service: grpc.ServiceDefinition } } } };
	};

	server.addService(loaded.shardrive.storage.v1.StorageAdapter.service, {
		upload: (call: grpc.ServerReadableStream<unknown, unknown>, callback: grpc.sendUnaryData<unknown>) => {
			void handleUpload(call, callback, backend);
		},
		download: (call: grpc.ServerWritableStream<unknown, unknown>) => {
			void handleDownload(call, backend);
		},
		delete: (call: grpc.ServerUnaryCall<unknown, unknown>, callback: grpc.sendUnaryData<unknown>) => {
			void handleDelete(call, callback, backend);
		},
		stat: (call: grpc.ServerUnaryCall<unknown, unknown>, callback: grpc.sendUnaryData<unknown>) => {
			void handleStat(call, callback, backend);
		},
		usage: (call: grpc.ServerUnaryCall<unknown, unknown>, callback: grpc.sendUnaryData<unknown>) => {
			void handleUsage(call, callback, backend);
		},
		health: (call: grpc.ServerUnaryCall<unknown, unknown>, callback: grpc.sendUnaryData<unknown>) => {
			void handleHealth(call, callback, backend);
		}
	});

	return server;
}

export async function startStorageAdapter(
	backend: StorageBackend,
	address = '127.0.0.1:0'
): Promise<{ server: grpc.Server; address: string; close: () => Promise<void> }> {
	const server = createStorageAdapterServer(backend);
	const port = await new Promise<number>((resolvePort, reject) => {
		server.bindAsync(address, grpc.ServerCredentials.createInsecure(), (error, boundPort) => {
			if (error) {
				reject(error);
				return;
			}
			resolvePort(boundPort);
		});
	});

	return {
		server,
		address: `127.0.0.1:${port}`,
		close: () => new Promise<void>((resolveClose) => server.tryShutdown(() => resolveClose()))
	};
}

async function handleUpload(
	call: grpc.ServerReadableStream<unknown, unknown>,
	callback: grpc.sendUnaryData<unknown>,
	backend: StorageBackend
): Promise<void> {
	const queue = new AsyncChunkQueue();
	let upload: Promise<{ objectId: string; sizeBytes: number }> | undefined;
	let resolveUploadStart: ((operation: Promise<{ objectId: string; sizeBytes: number }>) => void) | undefined;
	let rejectUploadStart: ((error: unknown) => void) | undefined;
	const uploadStarted = new Promise<Promise<{ objectId: string; sizeBytes: number }>>((resolveStart, rejectStart) => {
		resolveUploadStart = resolveStart;
		rejectUploadStart = rejectStart;
	});
	let failed = false;

	call.on('data', (rawMessage: unknown) => {
		try {
			const message = asRecord(rawMessage);
			if (message.start !== undefined) {
				if (upload) throw new StorageBackendError('INVALID_ARGUMENT', 'upload start frame must be first and unique');
				const start = asRecord(message.start);
				const input: UploadObject = {
					accountId: requiredString(start.account_id, 'account_id'),
					objectId: requiredString(start.object_id, 'object_id'),
					sizeBytes: requiredNonNegativeNumber(start.size_bytes, 'size_bytes'),
					chunks: queue
				};
				upload = backend.upload(input);
				resolveUploadStart?.(upload);
				return;
			}

			if (!upload) throw new StorageBackendError('INVALID_ARGUMENT', 'upload must begin with a start frame');
			const data = asBuffer(message.data, 'data');
			if (data.byteLength > MAX_DATA_FRAME_BYTES) {
				throw new StorageBackendError('INVALID_ARGUMENT', 'data frame exceeds adapter limit');
			}
			queue.push(data);
		} catch (error) {
			failed = true;
			rejectUploadStart?.(error);
			queue.fail(error);
			call.destroy();
			callback(toGrpcError(error));
		}
	});

	call.on('end', () => {
		if (!upload) {
			failed = true;
			const error = new StorageBackendError('INVALID_ARGUMENT', 'missing upload start frame');
			rejectUploadStart?.(error);
			queue.fail(error);
			callback(toGrpcError(error));
			return;
		}
		queue.end();
	});

	call.on('error', (error) => {
		if (!failed) queue.fail(error);
	});

	try {
		const result = await uploadStarted;
		if (!failed) callback(null, { object_id: result.objectId, size_bytes: result.sizeBytes });
	} catch (error) {
		if (!failed) callback(toGrpcError(error));
	}
}

async function handleDownload(
	call: grpc.ServerWritableStream<unknown, unknown>,
	backend: StorageBackend
): Promise<void> {
	try {
		const input = objectRequest(call.request);
		for await (const data of backend.download(input)) {
			if (data.byteLength > MAX_DATA_FRAME_BYTES) {
				throw new StorageBackendError('INTERNAL', 'backend returned an oversized data frame');
			}
			if (!call.write({ data })) await once(call, 'drain');
		}
		call.end();
	} catch (error) {
		call.destroy(toGrpcError(error));
	}
}

async function handleDelete(
	call: grpc.ServerUnaryCall<unknown, unknown>,
	callback: grpc.sendUnaryData<unknown>,
	backend: StorageBackend
): Promise<void> {
	try {
		await backend.delete(objectRequest(call.request));
		callback(null, {});
	} catch (error) {
		callback(toGrpcError(error));
	}
}

async function handleStat(
	call: grpc.ServerUnaryCall<unknown, unknown>,
	callback: grpc.sendUnaryData<unknown>,
	backend: StorageBackend
): Promise<void> {
	try {
		const result = await backend.stat(objectRequest(call.request));
		callback(null, {
			object_id: result.objectId,
			size_bytes: result.sizeBytes,
			modified_at: {
				seconds: Math.floor(result.modifiedAt.getTime() / 1000),
				nanos: (result.modifiedAt.getTime() % 1000) * 1_000_000
			}
		});
	} catch (error) {
		callback(toGrpcError(error));
	}
}

async function handleUsage(
	call: grpc.ServerUnaryCall<unknown, unknown>,
	callback: grpc.sendUnaryData<unknown>,
	backend: StorageBackend
): Promise<void> {
	try {
		const result = await backend.usage(accountRequest(call.request));
		callback(null, {
			total_bytes: result.totalBytes,
			used_bytes: result.usedBytes,
			free_bytes: result.freeBytes
		});
	} catch (error) {
		callback(toGrpcError(error));
	}
}

async function handleHealth(
	call: grpc.ServerUnaryCall<unknown, unknown>,
	callback: grpc.sendUnaryData<unknown>,
	backend: StorageBackend
): Promise<void> {
	try {
		callback(null, { healthy: await backend.health(accountRequest(call.request)) });
	} catch (error) {
		callback(toGrpcError(error));
	}
}

function objectRequest(raw: unknown): ObjectRequest {
	const request = asRecord(raw);
	return {
		accountId: requiredString(request.account_id, 'account_id'),
		objectId: requiredString(request.object_id, 'object_id')
	};
}

function accountRequest(raw: unknown): AccountRequest {
	const request = asRecord(raw);
	return { accountId: requiredString(request.account_id, 'account_id') };
}

function requiredString(value: unknown, name: string): string {
	if (typeof value !== 'string' || value.length === 0) {
		throw new StorageBackendError('INVALID_ARGUMENT', `${name} is required`);
	}
	return value;
}

function requiredNonNegativeNumber(value: unknown, name: string): number {
	if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) {
		throw new StorageBackendError('INVALID_ARGUMENT', `${name} must be a non-negative safe integer`);
	}
	return value;
}

function asBuffer(value: unknown, name: string): Buffer {
	if (!Buffer.isBuffer(value) && !(value instanceof Uint8Array)) {
		throw new StorageBackendError('INVALID_ARGUMENT', `${name} must be bytes`);
	}
	return Buffer.from(value);
}

function asRecord(value: unknown): Record<string, unknown> {
	if (!value || typeof value !== 'object' || Array.isArray(value)) {
		throw new StorageBackendError('INVALID_ARGUMENT', 'message must be an object');
	}
	return value as Record<string, unknown>;
}

function toGrpcError(error: unknown): grpc.ServiceError {
	const code = error instanceof StorageBackendError ? toGrpcStatus(error.code) : grpc.status.INTERNAL;
	const message = error instanceof Error ? error.message : 'storage adapter failure';
	return Object.assign(new Error(message), { code, details: message, metadata: new grpc.Metadata() });
}

function toGrpcStatus(code: StorageErrorCode): grpc.status {
	return {
		INVALID_ARGUMENT: grpc.status.INVALID_ARGUMENT,
		NOT_FOUND: grpc.status.NOT_FOUND,
		UNAUTHENTICATED: grpc.status.UNAUTHENTICATED,
		PERMISSION_DENIED: grpc.status.PERMISSION_DENIED,
		RESOURCE_EXHAUSTED: grpc.status.RESOURCE_EXHAUSTED,
		UNAVAILABLE: grpc.status.UNAVAILABLE,
		INTERNAL: grpc.status.INTERNAL
	}[code];
}

function requireGoogleProto(name: string): string {
	return require.resolve(`google-proto-files/${name}`);
}

class AsyncChunkQueue implements AsyncIterable<Buffer> {
	private readonly pending: Buffer[] = [];
	private readonly waiters: Array<(result: IteratorResult<Buffer>) => void> = [];
	private ended = false;
	private failure: unknown;

	push(chunk: Buffer): void {
		if (this.ended || this.failure) return;
		const waiter = this.waiters.shift();
		if (waiter) waiter({ done: false, value: chunk });
		else this.pending.push(chunk);
	}

	end(): void {
		if (this.ended || this.failure) return;
		this.ended = true;
		this.flushEnd();
	}

	fail(error: unknown): void {
		if (this.ended || this.failure) return;
		this.failure = error;
		while (this.waiters.length > 0) this.waiters.shift()?.({ done: true, value: undefined });
	}

	async *[Symbol.asyncIterator](): AsyncIterator<Buffer> {
		while (true) {
			if (this.failure) throw this.failure;
			const chunk = this.pending.shift();
			if (chunk) {
				yield chunk;
				continue;
			}
			if (this.ended) return;
			const result = await new Promise<IteratorResult<Buffer>>((resolveResult) => this.waiters.push(resolveResult));
			if (result.done) {
				if (this.failure) throw this.failure;
				return;
			}
			yield result.value;
		}
	}

	private flushEnd(): void {
		if (this.pending.length > 0) return;
		while (this.waiters.length > 0) this.waiters.shift()?.({ done: true, value: undefined });
	}
}
