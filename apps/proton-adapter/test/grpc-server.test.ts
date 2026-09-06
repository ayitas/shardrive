import assert from 'node:assert/strict';
import { test } from 'node:test';
import * as grpc from '@grpc/grpc-js';
import { loadStoragePackageDefinition, startStorageAdapter } from '../src/grpc-server.js';
import { StorageBackendError, type StorageBackend } from '../src/storage-backend.js';

test('gRPC adapter streams upload/download and translates backend errors', async () => {
	const objects = new Map<string, Buffer>();
	const backend: StorageBackend = {
		async upload(input) {
			const chunks: Buffer[] = [];
			for await (const chunk of input.chunks) chunks.push(chunk);
			const object = Buffer.concat(chunks);
			assert.equal(object.byteLength, input.sizeBytes);
			objects.set(`${input.accountId}/${input.objectId}`, object);
			return { objectId: input.objectId, sizeBytes: object.byteLength };
		},
		async *download(input) {
			const object = objects.get(`${input.accountId}/${input.objectId}`);
			if (!object) throw new StorageBackendError('NOT_FOUND', 'object not found');
			yield object.subarray(0, 3);
			yield object.subarray(3);
		},
		async delete(input) {
			if (!objects.delete(`${input.accountId}/${input.objectId}`)) {
				throw new StorageBackendError('NOT_FOUND', 'object not found');
			}
		},
		async stat(input) {
			const object = objects.get(`${input.accountId}/${input.objectId}`);
			if (!object) throw new StorageBackendError('NOT_FOUND', 'object not found');
			return { objectId: input.objectId, sizeBytes: object.byteLength, modifiedAt: new Date() };
		},
		async usage() {
			return { totalBytes: 100, usedBytes: 6, freeBytes: 94 };
		},
		async health() {
			return true;
		}
	};

	const adapter = await startStorageAdapter(backend);
	try {
		const definition = await loadStoragePackageDefinition();
		const loaded = grpc.loadPackageDefinition(definition) as unknown as {
			shardrive: { storage: { v1: { StorageAdapter: new (address: string, credentials: grpc.ChannelCredentials) => any } } };
		};
		const client = new loaded.shardrive.storage.v1.StorageAdapter(
			adapter.address,
			grpc.credentials.createInsecure()
		);

		const uploadResponse = await new Promise<any>((resolve, reject) => {
			const call = client.upload((error: Error | null, response: unknown) => {
				if (error) reject(error);
				else resolve(response);
			});
			call.write({ start: { account_id: 'account-1', object_id: 'object-1', size_bytes: 6 } });
			call.write({ data: Buffer.from('abc') });
			call.write({ data: Buffer.from('def') });
			call.end();
		});
		assert.deepEqual(uploadResponse, { object_id: 'object-1', size_bytes: 6 });

		const downloaded = await new Promise<Buffer>((resolve, reject) => {
			const chunks: Buffer[] = [];
			const call = client.download({ account_id: 'account-1', object_id: 'object-1' });
			call.on('data', (response: { data: Buffer }) => chunks.push(Buffer.from(response.data)));
			call.on('end', () => resolve(Buffer.concat(chunks)));
			call.on('error', reject);
		});
		assert.equal(downloaded.toString(), 'abcdef');

		const usage = await unary(client.usage.bind(client), { account_id: 'account-1' });
		assert.deepEqual(usage, { total_bytes: 100, used_bytes: 6, free_bytes: 94 });

		await assert.rejects(
			() => unary(client.stat.bind(client), { account_id: 'account-1', object_id: 'missing' }),
			(error: grpc.ServiceError) => error.code === grpc.status.NOT_FOUND
		);

		client.close();
	} finally {
		await adapter.close();
	}
});

function unary(
	method: (request: unknown, callback: (error: grpc.ServiceError | null, response: unknown) => void) => void,
	request: unknown
): Promise<unknown> {
	return new Promise((resolve, reject) => {
		method(request, (error, response) => {
			if (error) reject(error);
			else resolve(response);
		});
	});
}
