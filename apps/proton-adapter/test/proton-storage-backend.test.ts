import assert from 'node:assert/strict';
import { test } from 'node:test';
import { ProtonStorageBackend, type ProtonDriveClientLike } from '../src/proton-storage-backend.js';

test('ProtonStorageBackend streams objects through the SDK contract', async () => {
	const objects = new Map<string, Buffer>();
	const client: ProtonDriveClientLike = {
		async getMyFilesRootFolder() { return { uid: 'root' }; },
		async getFileUploader(_parent, name) {
			return {
				async uploadFromStream(stream) {
					const reader = stream.getReader();
					const chunks: Buffer[] = [];
					while (true) {
						const next = await reader.read();
						if (next.done) break;
						chunks.push(Buffer.from(next.value));
					}
					objects.set(name, Buffer.concat(chunks));
					return { async completion() { return { nodeUid: `node-${name}`, nodeRevisionUid: 'revision' }; } };
				}
			};
		},
		async getFileDownloader(nodeUid) {
			return {
				getClaimedSizeInBytes() { return objects.get(nodeUid)?.byteLength; },
				downloadToStream(stream) {
					let completed!: () => void;
					const completion = new Promise<void>((resolve) => { completed = resolve; });
					void (async () => {
						const writer = stream.getWriter();
						await writer.write(objects.get(nodeUid)!.subarray(0, 2));
						await writer.write(objects.get(nodeUid)!.subarray(2));
						await writer.close();
						completed();
					})();
					return { completion: async () => completion };
				}
			};
		},
		async getNode(nodeUid) { return { uid: nodeUid, modificationTime: new Date(123), activeRevision: { claimedSize: objects.get('object')?.byteLength ?? 6 } }; },
		async *trashNodes() {},
		async *deleteNodes() {}
	};
	const backend = new ProtonStorageBackend(async () => ({ client, usage: async () => ({ totalBytes: 100, usedBytes: 6, freeBytes: 94 }), health: async () => {} }));

	const uploaded = await backend.upload({ accountId: 'account-1', objectId: 'object', sizeBytes: 6, chunks: chunksOf(Buffer.from('abcdef')) });
	assert.equal(uploaded.objectId, 'node-object');
	const downloaded: Buffer[] = [];
	for await (const chunk of backend.download({ accountId: 'account-1', objectId: 'object' })) downloaded.push(chunk);
	assert.equal(Buffer.concat(downloaded).toString(), 'abcdef');
	assert.deepEqual(await backend.stat({ accountId: 'account-1', objectId: 'object' }), { objectId: 'object', sizeBytes: 6, modifiedAt: new Date(123) });
	assert.deepEqual(await backend.usage({ accountId: 'account-1' }), { totalBytes: 100, usedBytes: 6, freeBytes: 94 });
	await assert.doesNotReject(() => backend.health({ accountId: 'account-1' }));
	await assert.doesNotReject(() => backend.delete({ accountId: 'account-1', objectId: 'object' }));
});

async function* chunksOf(value: Buffer): AsyncGenerator<Buffer> {
	yield value.subarray(0, 3);
	yield value.subarray(3);
}
