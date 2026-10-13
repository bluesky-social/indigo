import { execFileSync } from 'node:child_process';
import { appendFileSync, existsSync, readFileSync, readdirSync } from 'node:fs';

const defaultRegistries = ['aws', 'ghcr'];
const registryExceptions = new Map([
  ['beemo', ['ghcr']],
  ['bluepages', ['aws']],
  ['collectiondir', ['aws']],
  ['rainbow', ['aws']],
]);
// Tap has its own tag-release workflow; all other Dockerfile-bearing commands
// publish to both registries unless listed above.
const projects = readdirSync('cmd', { withFileTypes: true })
  .filter(entry => entry.isDirectory() && entry.name !== 'tap' && existsSync(`cmd/${entry.name}/Dockerfile`))
  .map(entry => entry.name)
  .sort();

const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8'));
const branch = process.env.GITHUB_REF_NAME;
let changedPaths;

// A newly created publishing branch has no previous tree to compare.
if (!/^0+$/.test(event.before)) {
  try {
    execFileSync('git', ['cat-file', '-e', `${event.before}^{commit}`], { stdio: 'ignore' });
  } catch {
    // The previous commit may no longer be reachable after a force push.
    execFileSync('git', ['fetch', '--no-tags', 'origin', event.before], { stdio: 'inherit' });
  }
  changedPaths = execFileSync('git', [
    'diff', '--name-only', '--no-renames', '-z', event.before, event.after,
  ], { encoding: 'utf8' }).split('\0').filter(Boolean);
}

const sharedInputs = new Set([
  'go.mod',
  'go.sum',
  '.dockerignore',
  '.github/workflows/container-publish.yaml',
  '.github/scripts/container-matrix.mjs',
]);
const sharedChanged = changedPaths === undefined || changedPaths.some(
  path => path.endsWith('.go') || sharedInputs.has(path),
);

const include = [];
for (const service of projects) {
  const dockerfile = `cmd/${service}/Dockerfile`;
  const serviceChanged = sharedChanged || changedPaths.some(path =>
    path.startsWith(`cmd/${service}/`),
  );
  if (!serviceChanged) continue;

  for (const registry of registryExceptions.get(service) ?? defaultRegistries) {
    if (branch !== 'main' && !(branch === 'bnewbold/automod' && service === 'hepa' && registry === 'ghcr')) {
      continue;
    }
    include.push({
      service,
      registry,
      dockerfile,
      image: registry === 'aws' ? service : process.env.GITHUB_REPOSITORY,
      tagPrefix: registry === 'aws' ? '' : `${service}:`,
    });
  }
}

appendFileSync(process.env.GITHUB_OUTPUT,
  `matrix=${JSON.stringify({ include })}\nhas-images=${include.length > 0}\n`,
);
console.log(`Selected ${include.length} container images: ${include.map(row => `${row.service}/${row.registry}`).join(', ')}`);
