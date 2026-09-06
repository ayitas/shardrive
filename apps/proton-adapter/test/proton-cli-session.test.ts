import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
	InvalidProtonCliSessionError,
	importProtonCliSessionSnapshot,
	parseProtonCliSessionSnapshot
} from '../src/proton-cli-session.js';

test('parses the official CLI session fields without retaining unknown fields', () => {
	const session = parseProtonCliSessionSnapshot(
		JSON.stringify({
			uid: 'uid-1',
			accessToken: 'access-token',
			refreshToken: 'refresh-token',
			userKeyPassword: 'user-key-password',
			cachePassword: 'cache-password',
			telemetryEnabled: false,
			unexpectedSecret: 'ignored'
		})
	);

	assert.deepEqual(session, {
		uid: 'uid-1',
		accessToken: 'access-token',
		refreshToken: 'refresh-token',
		userKeyPassword: 'user-key-password',
		cachePassword: 'cache-password',
		telemetryEnabled: false
	});
});

test('rejects malformed CLI sessions before writing them', async () => {
	let writes = 0;
	const store = { put: async () => void writes++ };

	assert.equal(parseProtonCliSessionSnapshot('{"uid":"uid-1"}'), undefined);
	await assert.rejects(
		() => importProtonCliSessionSnapshot('account-1', '{"uid":"uid-1"}', store),
		(error: InvalidProtonCliSessionError) => error instanceof InvalidProtonCliSessionError
	);
	assert.equal(writes, 0);
});

test('imports a valid CLI session directly into the encrypted store boundary', async () => {
	let stored: Record<string, unknown> | undefined;
	const store = {
		put: async (_accountId: string, payload: Record<string, unknown>) => {
			stored = payload;
		}
	};

	await importProtonCliSessionSnapshot(
		'account-1',
		JSON.stringify({ uid: 'uid-1', accessToken: 'access-token', userKeyPassword: 'user-key-password' }),
		store
	);
	assert.deepEqual(stored, { uid: 'uid-1', accessToken: 'access-token', userKeyPassword: 'user-key-password' });
});
