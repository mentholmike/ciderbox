package cloudflaresandbox

import "flag"

type cloudflareSandboxFlagValues struct {
	APIURL  *string
	Token   *string
	Workdir *string
}

func RegisterCloudflareSandboxProviderFlags(fs *flag.FlagSet, defaults Config) any {
	return cloudflareSandboxFlagValues{
		APIURL:  fs.String("cloudflare-sandbox-url", defaults.CloudflareSandbox.APIURL, "Cloudflare Sandbox runner API URL"),
		Token:   fs.String("cloudflare-sandbox-token", "", "Cloudflare Sandbox runner bearer token"),
		Workdir: fs.String("cloudflare-sandbox-workdir", defaults.CloudflareSandbox.Workdir, "Absolute working directory inside the Cloudflare sandbox"),
	}
}

func ApplyCloudflareSandboxProviderFlags(cfg *Config, fs *flag.FlagSet, values any) error {
	if cfg.Provider == providerName || cfg.Provider == "cf-sandbox" || cfg.Provider == "cf-containers" || cfg.Provider == "cloudflare" || cfg.Provider == "cloudflare-containers" {
		if flagWasSet(fs, "class") {
			return exit(2, "--class is not supported for provider=%s", providerName)
		}
		if flagWasSet(fs, "type") {
			return exit(2, "--type is not supported for provider=%s", providerName)
		}
	}
	v, ok := values.(cloudflareSandboxFlagValues)
	if !ok {
		return nil
	}
	if flagWasSet(fs, "cloudflare-sandbox-url") {
		cfg.CloudflareSandbox.APIURL = *v.APIURL
	}
	if flagWasSet(fs, "cloudflare-sandbox-token") {
		cfg.CloudflareSandbox.Token = *v.Token
	}
	if flagWasSet(fs, "cloudflare-sandbox-workdir") {
		cfg.CloudflareSandbox.Workdir = *v.Workdir
	}
	return nil
}
