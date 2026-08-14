import type { LoaderFunction } from '@remix-run/node';
import { json } from '@remix-run/node';

export const loader: LoaderFunction = async ({ request }) => {
  try {
    const source = new URL(request.url);
    const backendBaseUrl = process.env.BACKEND_URL || 'http://localhost:3001';
    const backendUrl = new URL('/api/sessions', backendBaseUrl);
    source.searchParams.forEach((value, key) => backendUrl.searchParams.append(key, value));

    const response = await fetch(backendUrl.toString());
    if (!response.ok) {
      return json({ error: await response.text() }, { status: response.status });
    }
    return json(await response.json());
  } catch (error) {
    console.error('Failed to fetch sessions:', error);
    return json({ sessions: [], total: 0 }, { status: 502 });
  }
};
