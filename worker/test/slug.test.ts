import { describe, expect, it } from "vitest";

import {
  leaseProviderName,
  leaseSlugFromID,
  normalizeLeaseSlug,
  slugWithCollisionSuffix,
} from "../src/slug";

describe("lease slugs", () => {
  it("generates deterministic DNS-ish slugs", () => {
    const slug = leaseSlugFromID("cbx_abcdef123456");
    expect(leaseSlugFromID("cbx_abcdef123456")).toBe(slug);
    expect(slug).toMatch(/^[a-z0-9]+-[a-z0-9]+$/);
    expect(leaseProviderName("cbx_abcdef123456", slug).length).toBeLessThanOrEqual(63);
  });

  it("matches Go golden fixtures", () => {
    expect(leaseSlugFromID("cbx_000000000001")).toBe("tidal-lobster");
    expect(leaseSlugFromID("cbx_abcdef123456")).toBe("blue-prawn");
    expect(leaseSlugFromID("cbx_deadbeefcafe")).toBe("silver-crab");
  });

  it("normalizes requested slugs and appends collision suffixes", () => {
    expect(normalizeLeaseSlug(" Blue Lobster ")).toBe("blue-lobster");
    expect(normalizeLeaseSlug(" --- Blue__Lobster!! ")).toBe("blue-lobster");
    expect(slugWithCollisionSuffix("Blue Lobster", "cbx_abcdef123456")).toMatch(
      /^blue-lobster-[a-f0-9]{4}$/,
    );
  });

  it("uses slug for provider names while preserving ID fallback", () => {
    expect(leaseProviderName("cbx_abcdef123456", "blue-lobster")).toBe(
      "crabbox-blue-lobster-c80c2195",
    );
    expect(leaseProviderName("cbx_abcdef123456", "")).toBe("crabbox-cbx-abcdef123456");
  });
});
