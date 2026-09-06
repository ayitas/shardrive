import { randomBytes } from 'node:crypto';
import { chmod, mkdir } from 'node:fs/promises';
import { FileSessionStore, SessionVault } from '../src/session-vault.js';
import { parseProtonCliSessionSnapshot } from '../src/proton-cli-session.js';

const accountRef = requiredEnv('SHARDRIVE_PROTON_ACCOUNT_REF');
const sessionRoot = requiredEnv('SHARDRIVE_PROTON_SESSION_ROOT');
const masterKeyBase64 = requiredEnv('SHARDRIVE_PROTON_MASTER_KEY_B64');
const key = Buffer.from(masterKeyBase64, 'base64');
if (key.byteLength !== 32) throw new Error('SHARDRIVE_PROTON_MASTER_KEY_B64 must decode to 32 bytes');

const raw = await Bun.secrets.get({ service: 'ch.proton.drive/drive-sdk-cli', name: 'auth-session' });
if (!raw) throw new Error('official Proton CLI session not found in the OS keychain');
const snapshot = parseProtonCliSessionSnapshot(raw);
if (!snapshot) throw new Error('official Proton CLI session snapshot is invalid');

await mkdir(sessionRoot, { recursive: true, mode: 0o700 });
await chmod(sessionRoot, 0o700);
await new FileSessionStore(sessionRoot, new SessionVault(key)).put(accountRef, snapshot);
console.log(`encrypted Proton session imported: account_ref=${accountRef} session_root=${sessionRoot}`);

function requiredEnv(name: string): string {
	const value = process.env[name];
	if (!value) throw new Error(`${name} is required`);
	return value;
}
