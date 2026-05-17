import { describe, expect, it } from "vitest";

import { renderTailscaleHostname } from "../src/tailscale";

describe("tailscale hostnames", () => {
  it("renders DNS labels from templates", () => {
    expect(
      renderTailscaleHostname(
        " Crabbox/{provider}/{slug}/{id} ",
        "cbx_abcdef123456",
        "Blue Lobster",
        "hetzner",
      ),
    ).toBe("crabbox-hetzner-blue-lobster-cbx-abcdef123456");
  });

  it("falls back to the lease id when the rendered hostname is empty", () => {
    expect(renderTailscaleHostname("!!!", "cbx_abcdef123456", "", "aws")).toBe(
      "crabbox-cbx-abcdef123456",
    );
  });
});
