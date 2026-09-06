import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
	ProtonAuthRequiredError,
	createProtonDriveClientForAccount,
	type ProtonSDKRuntime,
	type ProtonSessionProvider
} from '../src/proton-client-factory.js';

type TestParameters = { sessionId: string };
type TestClient = { sessionId: string };

test('factory loads account session before constructing the SDK client', async () => {
	const runtime: ProtonSDKRuntime<TestClient, TestParameters> = {
		ProtonDriveClient: class {
			constructor(parameters: TestParameters) {
				return { sessionId: parameters.sessionId };
			}
		} as unknown as ProtonSDKRuntime<TestClient, TestParameters>['ProtonDriveClient']
	};
	const sessions: ProtonSessionProvider<TestParameters> = {
		async load(accountId) {
			assert.equal(accountId, 'account-1');
			return { sessionId: 'session-1' };
		}
	};

	assert.deepEqual(await createProtonDriveClientForAccount(runtime, sessions, 'account-1'), {
		sessionId: 'session-1'
	});
});

test('factory fails explicitly when the account needs authentication', async () => {
	const runtime = {
		ProtonDriveClient: class {}
	} as unknown as ProtonSDKRuntime<object, TestParameters>;
	const sessions: ProtonSessionProvider<TestParameters> = {
		async load() {
			return undefined;
		}
	};

	await assert.rejects(
		() => createProtonDriveClientForAccount(runtime, sessions, 'account-1'),
		(error: ProtonAuthRequiredError) =>
			error instanceof ProtonAuthRequiredError &&
			error.accountId === 'account-1' &&
			error.message === 'Proton session is required for account account-1'
	);
});
