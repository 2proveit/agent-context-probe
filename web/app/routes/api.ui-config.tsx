import type { LoaderFunction } from "@remix-run/node";
import { json } from "@remix-run/node";

export const loader: LoaderFunction = async () => {
  try {
    const backendBaseUrl = process.env.BACKEND_URL || "http://localhost:3001";
    const response = await fetch(new URL("/api/ui-config", backendBaseUrl));
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return json(await response.json());
  } catch (error) {
    console.error("Failed to fetch UI config:", error);
    return json({
      showRawStreamEvents: false,
      rawRequestMaxDisplayChars: 0,
      rawResponseMaxDisplayChars: 0,
    });
  }
};
