import { cp, mkdir, rm } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const source = resolve(webRoot, "dist");
const embedRoot = resolve(webRoot, "../proxy/internal/webui/dist");
const destination = resolve(embedRoot, "client");

await rm(destination, { recursive: true, force: true });
await rm(resolve(embedRoot, "server"), { recursive: true, force: true });
await mkdir(embedRoot, { recursive: true });
await cp(source, destination, { recursive: true });
