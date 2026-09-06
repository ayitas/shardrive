import { mkdtemp, readFile, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { FileSessionStore, SessionVault } from '../src/session-vault.js';

test('session survives adapter store recreation without storing plaintext', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'shardrive-proton-adapter-'));
	const accountId = 'account-1';
	const payload = {
		accessToken: 'secret-token-for-test',
		uid: 'session-uid',
		createdAt: '2026-09-06T00:00:00.000Z'
	};
	const masterKey = Buffer.alloc(32, 7);

	await new FileSessionStore(directory, new SessionVault(masterKey)).put(accountId, payload);
	const stored = await readFile(join(directory, `${accountId}.session`), 'utf8');
	assert.equal(stored.includes('secret-token-for-test'), false);
	assert.deepEqual(await new FileSessionStore(directory, new SessionVault(masterKey)).get(accountId), payload);
	assert.equal((await stat(join(directory, `${accountId}.session`))).mode & 0o777, 0o600);
});

test('wrong master key cannot open a stored session', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'shardrive-proton-adapter-'));
	const accountId = 'account-1';
	await new FileSessionStore(directory, new SessionVault(Buffer.alloc(32, 1))).put(accountId, {
		accessToken: 'secret-token-for-test'
	});

	await assert.rejects(
		() => new FileSessionStore(directory, new SessionVault(Buffer.alloc(32, 2))).get(accountId),
		/unable to decrypt session envelope/
	);
});

test('rejects account ids that could escape the session directory', async () => {
	const directory = await mkdtemp(join(tmpdir(), 'shardrive-proton-adapter-'));
	const store = new FileSessionStore(directory, new SessionVault(Buffer.alloc(32, 1)));

	await assert.rejects(() => store.put('../outside', { accessToken: 'secret' }), /invalid storage account id/);
});
