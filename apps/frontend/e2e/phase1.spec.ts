import { test, expect } from '@playwright/test';
import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';

test('Phase 1 upload, download, and delete gate', async ({ page }) => {
	const email = process.env.E2E_EMAIL;
	const password = process.env.E2E_PASSWORD;
	if (!email || !password) {
		throw new Error('E2E_EMAIL and E2E_PASSWORD must be set; credentials are never stored in the repository.');
	}

	const fileName = `playwright-e2e-${Date.now()}.bin`;
	const payload = Buffer.alloc(1024 * 1024, 0x5a);
	const originalHash = createHash('sha256').update(payload).digest('hex');

	await page.setViewportSize({ width: 390, height: 844 });
	await page.goto('/');
	await page.getByLabel('Email').fill(email);
	await page.getByLabel('Password').fill(password);
	await page.getByRole('button', { name: 'Sign in' }).click();
	await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
	const sortSelect = page.getByLabel('Sort files and folders');
	await sortSelect.selectOption('name-asc');
	await expect(sortSelect).toHaveValue('name-asc');
	const folderName = `playwright-folder-${Date.now()}`;
	await page.getByLabel('New folder name').fill(folderName);
	await page.getByRole('button', { name: 'New folder', exact: true }).click();
	const renamedFolderName = `${folderName}-renamed`;
	const folderRow = page.locator('li').filter({ hasText: folderName });
	await page.once('dialog', (dialog) => dialog.accept(renamedFolderName));
	await folderRow.getByRole('button', { name: 'Rename' }).click();
	const folderButton = page.getByRole('button', { name: new RegExp(renamedFolderName) });
	await expect(folderButton).toBeVisible();
	await folderButton.click();

	const fileInput = page.locator('input[type="file"]');
	await fileInput.setInputFiles({ name: fileName, mimeType: 'application/octet-stream', buffer: payload });
	await page.getByRole('button', { name: 'Upload', exact: true }).click();

	const fileRow = page.locator('li').filter({ hasText: fileName });
	await expect(fileRow).toContainText('AVAILABLE', { timeout: 60_000 });
	const searchInput = page.getByLabel('Search files');
	await searchInput.fill(fileName);
	await expect(fileRow).toBeVisible();
	await searchInput.fill('does-not-match');
	await expect(fileRow).toHaveCount(0);
	await searchInput.fill('');
	await expect(fileRow).toBeVisible();

	const downloadPromise = page.waitForEvent('download');
	await fileRow.getByRole('link', { name: 'Download' }).click();
	const download = await downloadPromise;
	const downloadPath = await download.path();
	expect(downloadPath).not.toBeNull();
	const downloaded = await readFile(downloadPath!);
	const downloadedHash = createHash('sha256').update(downloaded).digest('hex');
	expect(downloadedHash).toBe(originalHash);
	expect(downloaded.equals(payload)).toBe(true);

	await page.once('dialog', (dialog) => dialog.accept());
	await fileRow.getByRole('button', { name: 'Delete' }).click();
	await expect.poll(async () => {
		await page.reload();
		return (await page.locator('section[aria-live="polite"]').innerText()).includes('0 B /');
	}, { timeout: 30_000, intervals: [500, 1_000, 2_000] }).toBe(true);
	await expect(fileRow).toHaveCount(0, { timeout: 30_000 });
	const renamedFolderRow = page.locator('li').filter({ hasText: renamedFolderName });
	await expect(renamedFolderRow).toBeVisible();
	await page.once('dialog', (dialog) => dialog.accept());
	await renamedFolderRow.getByRole('button', { name: 'Delete' }).click();
	await expect(renamedFolderRow).toHaveCount(0, { timeout: 30_000 });
});
