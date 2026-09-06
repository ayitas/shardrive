import { test, expect } from '@playwright/test';

test('one-account Proton connect and refresh gate', async ({ page }) => {
	const email = process.env.E2E_EMAIL;
	const password = process.env.E2E_PASSWORD;
	const accountName = process.env.E2E_PROTON_ACCOUNT_NAME;
	const accountReference = process.env.E2E_PROTON_ACCOUNT_REF;
	test.skip(!email || !password || !accountName || !accountReference, 'Set E2E_EMAIL, E2E_PASSWORD, E2E_PROTON_ACCOUNT_NAME, and E2E_PROTON_ACCOUNT_REF for the live Proton gate.');

	await page.goto('/');
	await page.getByLabel('Email').fill(email!);
	await page.getByLabel('Password').fill(password!);
	await page.getByRole('button', { name: 'Sign in' }).click();
	await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();

	const accountsCard = page.getByLabel('Storage accounts');
	const existingAccount = accountsCard.locator('li').filter({ hasText: accountName! });
	if (await existingAccount.count() === 0) {
		await page.getByLabel('Proton account name').fill(accountName!);
		await page.getByLabel('Session reference').fill(accountReference!);
		await page.getByRole('button', { name: 'Connect Proton account' }).click();
	}

	const account = accountsCard.locator('li').filter({ hasText: accountName! }).first();
	await expect(account).toContainText('proton');
	await expect(account.locator('.status-pill')).toContainText('ACTIVE', { timeout: 30_000 });
	await account.getByRole('button', { name: 'Refresh', exact: true }).click();
	await expect(account.locator('.status-pill')).toContainText('ACTIVE', { timeout: 30_000 });
	await expect(account).not.toContainText(accountReference!);
});
