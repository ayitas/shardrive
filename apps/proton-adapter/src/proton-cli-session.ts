import { readFile } from 'node:fs/promises';
import type { FileSessionStore, SessionPayload } from './session-vault.js';

export interface ProtonCliSession extends SessionPayload {
	uid: string;
	accessToken: string;
	userKeyPassword: string;
	refreshToken?: string;
	cachePassword?: string;
	telemetryEnabled?: boolean;
}

export class InvalidProtonCliSessionError extends Error {
	constructor() {
		super('invalid Proton CLI session snapshot');
		this.name = 'InvalidProtonCliSessionError';
	}
}

export function parseProtonCliSessionSnapshot(raw: string): ProtonCliSession | undefined {
	let value: unknown;
	try {
		value = JSON.parse(raw);
	} catch {
		return undefined;
	}

	if (!isRecord(value) || !isNonEmptyString(value.uid) || !isNonEmptyString(value.accessToken)) {
		return undefined;
	}
	if (!isNonEmptyString(value.userKeyPassword)) return undefined;
	if (value.refreshToken !== undefined && !isNonEmptyString(value.refreshToken)) return undefined;
	if (value.cachePassword !== undefined && !isNonEmptyString(value.cachePassword)) return undefined;
	if (value.telemetryEnabled !== undefined && typeof value.telemetryEnabled !== 'boolean') return undefined;

	const session: ProtonCliSession = {
		uid: value.uid,
		accessToken: value.accessToken,
		userKeyPassword: value.userKeyPassword
	};
	if (value.refreshToken !== undefined) session.refreshToken = value.refreshToken;
	if (value.cachePassword !== undefined) session.cachePassword = value.cachePassword;
	if (value.telemetryEnabled !== undefined) session.telemetryEnabled = value.telemetryEnabled;
	return session;
}

export async function importProtonCliSessionSnapshot(
	accountId: string,
	raw: string,
	store: Pick<FileSessionStore, 'put'>
): Promise<void> {
	const session = parseProtonCliSessionSnapshot(raw);
	if (!session) throw new InvalidProtonCliSessionError();
	await store.put(accountId, session);
}

export async function importProtonCliSessionFile(
	accountId: string,
	path: string,
	store: Pick<FileSessionStore, 'put'>
): Promise<void> {
	await importProtonCliSessionSnapshot(accountId, await readFile(path, 'utf8'), store);
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function isNonEmptyString(value: unknown): value is string {
	return typeof value === 'string' && value.length > 0;
}
