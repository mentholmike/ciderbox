const adjectives = [
  "amber",
  "blue",
  "brisk",
  "coral",
  "crimson",
  "golden",
  "harbor",
  "jade",
  "pearl",
  "quick",
  "silver",
  "swift",
  "tidal",
  "violet",
];

const nouns = ["barnacle", "crab", "crayfish", "hermit", "krill", "lobster", "prawn", "shrimp"];

export function leaseSlugFromID(leaseID: string): string {
  const hash = slugHash(leaseID);
  const adjective = adjectives[hash % adjectives.length] ?? "blue";
  const noun = nouns[Math.trunc(hash / adjectives.length) % nouns.length] ?? "crab";
  return `${adjective}-${noun}`;
}

export function normalizeLeaseSlug(value: string | undefined): string {
  let out = "";
  let lastDash = false;
  for (const char of (value ?? "").trim().toLowerCase()) {
    const code = char.charCodeAt(0);
    const ok = (code >= 97 && code <= 122) || (code >= 48 && code <= 57);
    if (ok) {
      out += char;
      lastDash = false;
      continue;
    }
    if (!lastDash) {
      out += "-";
      lastDash = true;
    }
  }
  return trimDashes(out);
}

export function slugWithCollisionSuffix(base: string, seed: string): string {
  const normalized = normalizeLeaseSlug(base) || leaseSlugFromID(seed);
  return `${normalized}-${(slugHash(seed) & 0xffff).toString(16).padStart(4, "0")}`;
}

export function leaseProviderName(leaseID: string, slug: string | undefined): string {
  const normalized = normalizeLeaseSlug(slug);
  return normalized
    ? `crabbox-${normalized}-${slugHash(leaseID).toString(16).padStart(8, "0")}`
    : `crabbox-${leaseID}`.replaceAll("_", "-");
}

function slugHash(value: string): number {
  let hash = 0x811c9dc5;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash >>> 0;
}

function trimDashes(value: string): string {
  let start = 0;
  let end = value.length;
  while (start < end && value[start] === "-") {
    start += 1;
  }
  while (end > start && value[end - 1] === "-") {
    end -= 1;
  }
  return value.slice(start, end);
}
