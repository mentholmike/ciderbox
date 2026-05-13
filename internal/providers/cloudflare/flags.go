package cloudflare

import (
	"flag"
	"strings"
)

type cfContainersFlagValues struct {
	APIURL  *string
	Token   *string
	Workdir *string
}

func RegisterCFContainersProviderFlags(fs *flag.FlagSet, defaults Config) any {
	return cfContainersFlagValues{
		APIURL:  fs.String("cf-containers-url", defaults.CFContainers.APIURL, "CF Containers runner API URL"),
		Token:   fs.String("cf-containers-token", "", "CF Containers runner bearer token"),
		Workdir: fs.String("cf-containers-workdir", defaults.CFContainers.Workdir, "Absolute working directory inside the CF Containers workspace"),
	}
}

func ApplyCFContainersProviderFlags(cfg *Config, fs *flag.FlagSet, values any) error {
	if isCFContainersProviderName(cfg.Provider) {
		if flagWasSet(fs, "class") {
			return exit(2, "--class is not supported for provider=%s", providerName)
		}
		if flagWasSet(fs, "type") {
			return exit(2, "--type is not supported for provider=%s", providerName)
		}
	}
	v, ok := values.(cfContainersFlagValues)
	if !ok {
		return nil
	}
	if flagWasSet(fs, "cf-containers-url") {
		cfg.CFContainers.APIURL = *v.APIURL
	}
	if flagWasSet(fs, "cf-containers-token") {
		cfg.CFContainers.Token = *v.Token
	}
	if flagWasSet(fs, "cf-containers-workdir") {
		cfg.CFContainers.Workdir = *v.Workdir
	}
	return nil
}

func isCFContainersProviderName(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case providerName, "cloudflare-containers", cloudflareContainerName, "cf-container":
		return true
	default:
		return false
	}
}
