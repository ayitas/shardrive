import { randomBytes, createCipheriv, createDecipheriv } from 'node:crypto';
import { chmod, mkdir, readFile, rename, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

const ALGORITHM = 'aes-256-gcm';
const IV_BYTES = 12;
const KEY_BYTES = 32;
const ENVELOPE_VERSION = 1;
const ACCOUNT_ID_PATTERN = /^[A-Za-z0-9_-]+$/;

interface SessionEnvelope {
	version: number;
	iv: string;
	tag: string;
	ciphertext: string;
}

export type SessionPayload = Record<string, unknown>;

export class SessionVault {
	private readonly key: Buffer;

	constructor(masterKey: Uint8Array) {
		if (masterKey.byteLength !== KEY_BYTES) {
			throw new Error(`session vault key must be ${KEY_BYTES} bytes`);
		}

		this.key = Buffer.from(masterKey);
	}

	seal(accountId: string, payload: SessionPayload): string {
		const iv = randomBytes(IV_BYTES);
		const cipher = createCipheriv(ALGORITHM, this.key, iv);
		cipher.setAAD(Buffer.from(accountId, 'utf8'));
		const ciphertext = Buffer.concat([
			cipher.update(JSON.stringify(payload), 'utf8'),
			cipher.final()
		]);
		const envelope: SessionEnvelope = {
			version: ENVELOPE_VERSION,
			iv: iv.toString('base64url'),
			tag: cipher.getAuthTag().toString('base64url'),
			ciphertext: ciphertext.toString('base64url')
		};

		return JSON.stringify(envelope);
	}

	open(accountId: string, serializedEnvelope: string): SessionPayload {
		let envelope: SessionEnvelope;
		try {
			envelope = JSON.parse(serializedEnvelope) as SessionEnvelope;
		} catch (error) {
			throw new Error('invalid session envelope', { cause: error });
		}

		if (envelope.version !== ENVELOPE_VERSION) {
			throw new Error('unsupported session envelope version');
		}

		try {
			const decipher = createDecipheriv(ALGORITHM, this.key, Buffer.from(envelope.iv, 'base64url'));
			decipher.setAAD(Buffer.from(accountId, 'utf8'));
			decipher.setAuthTag(Buffer.from(envelope.tag, 'base64url'));
			const plaintext = Buffer.concat([
				decipher.update(Buffer.from(envelope.ciphertext, 'base64url')),
				decipher.final()
			]).toString('utf8');
			const payload = JSON.parse(plaintext) as unknown;
			if (!payload || typeof payload !== 'object' || Array.isArray(payload)) {
				throw new Error('session payload must be an object');
			}
			return payload as SessionPayload;
		} catch (error) {
			throw new Error('unable to decrypt session envelope', { cause: error });
		}
	}
}

export class FileSessionStore {
	constructor(
		private readonly rootDirectory: string,
		private readonly vault: SessionVault
	) {}

	async put(accountId: string, payload: SessionPayload): Promise<void> {
		const path = this.pathFor(accountId);
		await mkdir(this.rootDirectory, { recursive: true, mode: 0o700 });
		await chmod(this.rootDirectory, 0o700);
		const temporaryPath = `${path}.${process.pid}.${randomBytes(6).toString('hex')}.tmp`;
		await writeFile(temporaryPath, this.vault.seal(accountId, payload), {
			encoding: 'utf8',
			mode: 0o600
		});
		await rename(temporaryPath, path);
	}

	async get(accountId: string): Promise<SessionPayload | undefined> {
		const path = this.pathFor(accountId);
		try {
			return this.vault.open(accountId, await readFile(path, 'utf8'));
		} catch (error) {
			if (isMissingFile(error)) {
				return undefined;
			}
			throw error;
		}
	}

	async delete(accountId: string): Promise<void> {
		await rm(this.pathFor(accountId), { force: true });
	}

	private pathFor(accountId: string): string {
		if (!ACCOUNT_ID_PATTERN.test(accountId)) {
			throw new Error('invalid storage account id');
		}
		return join(this.rootDirectory, `${accountId}.session`);
	}
}

function isMissingFile(error: unknown): boolean {
	return typeof error === 'object' && error !== null && 'code' in error && error.code === 'ENOENT';
}
