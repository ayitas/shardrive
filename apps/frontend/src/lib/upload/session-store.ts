export interface PersistedUploadSession { uploadId: string; fileId: string; name: string; size: number; chunkSize: number; chunkCount: number; completedIndexes: number[]; updatedAt: string }

const databaseName = 'shardrive';
const storeName = 'uploads';

export async function saveUploadSession(session: PersistedUploadSession): Promise<void> {
	if (typeof indexedDB === 'undefined') return;
	const db = await openDatabase();
	await new Promise<void>((resolve, reject) => {
		const request = db.transaction(storeName, 'readwrite').objectStore(storeName).put(session);
		request.onsuccess = () => resolve(); request.onerror = () => reject(request.error);
	});
	db.close();
}

export async function listUploadSessions(): Promise<PersistedUploadSession[]> {
	if (typeof indexedDB === 'undefined') return [];
	const db = await openDatabase();
	const result = await new Promise<PersistedUploadSession[]>((resolve, reject) => {
		const request = db.transaction(storeName, 'readonly').objectStore(storeName).getAll();
		request.onsuccess = () => resolve((request.result as PersistedUploadSession[]) ?? []); request.onerror = () => reject(request.error);
	});
	db.close(); return result;
}

export async function deleteUploadSession(uploadId: string): Promise<void> {
	if (typeof indexedDB === 'undefined') return;
	const db = await openDatabase();
	await new Promise<void>((resolve, reject) => {
		const request = db.transaction(storeName, 'readwrite').objectStore(storeName).delete(uploadId);
		request.onsuccess = () => resolve(); request.onerror = () => reject(request.error);
	});
	db.close();
}

function openDatabase(): Promise<IDBDatabase> {
	return new Promise((resolve, reject) => {
		const request = indexedDB.open(databaseName, 1);
		request.onupgradeneeded = () => request.result.createObjectStore(storeName, { keyPath: 'uploadId' });
		request.onsuccess = () => resolve(request.result); request.onerror = () => reject(request.error);
	});
}
