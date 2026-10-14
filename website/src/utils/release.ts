import { GITHUB_REPO } from '../constants';

export interface Release {
	tag: string;
	url: string;
}

// The GitHub API endpoint that returns the latest published (non-draft, non-prerelease) release.
export const LATEST_RELEASE_API = GITHUB_REPO.replace('https://github.com/', 'https://api.github.com/repos/') + '/releases/latest';

let latestRelease: Promise<Release | null> | undefined;

/**
 * Returns the latest CLI release, fetched once per build. Returns null if it can't be fetched,
 * so the build never fails because of GitHub API errors or rate limits.
 */
export function getLatestRelease(): Promise<Release | null> {
	latestRelease ??= fetchLatestRelease();
	return latestRelease;
}

async function fetchLatestRelease(): Promise<Release | null> {
	try {
		const headers: Record<string, string> = { Accept: 'application/vnd.github+json' };
		if (process.env.GITHUB_TOKEN) {
			headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
		}
		const res = await fetch(LATEST_RELEASE_API, { headers, signal: AbortSignal.timeout(5000) });
		if (!res.ok) {
			console.warn(`Could not fetch the latest release: ${res.status} ${res.statusText}`);
			return null;
		}
		const data = await res.json();
		return { tag: data.tag_name, url: data.html_url };
	} catch (err) {
		console.warn(`Could not fetch the latest release: ${err}`);
		return null;
	}
}
