package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (a App) image(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return exit(2, "usage: crabbox image current|list|create")
	}
	switch args[0] {
	case "current":
		return a.imageCurrent(ctx, args[1:])
	case "list":
		return a.imageList(ctx, args[1:])
	case "create":
		return a.imageCreate(ctx, args[1:])
	default:
		return exit(2, "unknown image command %q", args[0])
	}
}

func (a App) imageCurrent(ctx context.Context, args []string) error {
	fs := newFlagSet("image current", a.Stderr)
	provider := fs.String("provider", "aws", "provider: aws")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	cfg, err := imageAWSConfig(*provider)
	if err != nil {
		return err
	}
	image, err := currentAWSImage(ctx, cfg)
	if err != nil {
		return err
	}
	return printAWSImage(a.Stdout, image, *jsonOut)
}

func (a App) imageList(ctx context.Context, args []string) error {
	fs := newFlagSet("image list", a.Stderr)
	provider := fs.String("provider", "aws", "provider: aws")
	name := fs.String("name", "", "AMI name glob; defaults to Crabbox-tagged images")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	cfg, err := imageAWSConfig(*provider)
	if err != nil {
		return err
	}
	images, err := listAWSImages(ctx, cfg, *name)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(a.Stdout).Encode(images)
	}
	for _, image := range images {
		fmt.Fprintf(a.Stdout, "%-20s %-10s %-20s %s\n", image.ID, blank(image.State, "-"), blank(image.CreationDate, "-"), image.Name)
	}
	return nil
}

func (a App) imageCreate(ctx context.Context, args []string) error {
	fs := newFlagSet("image create", a.Stderr)
	provider := fs.String("provider", "aws", "provider: aws")
	id := fs.String("id", "", "source lease id or slug")
	name := fs.String("name", "", "AMI name")
	description := fs.String("description", "", "AMI description")
	noReboot := fs.Bool("no-reboot", false, "avoid rebooting before image capture; faster but less consistent")
	wait := fs.Bool("wait", false, "wait until the AMI becomes available")
	skipScrub := fs.Bool("skip-scrub", false, "skip best-effort secret scrub on the source runner")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *id == "" && fs.NArg() > 0 {
		*id = fs.Arg(0)
	}
	if *id == "" || *name == "" {
		return exit(2, "usage: crabbox image create --id <lease-id-or-slug> --name <ami-name>")
	}
	cfg, err := imageAWSConfig(*provider)
	if err != nil {
		return err
	}
	server, target, leaseID, err := a.resolveLeaseTarget(ctx, cfg, *id)
	if err != nil {
		return err
	}
	if server.Provider != "" && server.Provider != "aws" {
		return exit(2, "image create only supports AWS leases, got provider=%s", server.Provider)
	}
	if server.CloudID == "" || !strings.HasPrefix(server.CloudID, "i-") {
		return exit(2, "source lease does not have an AWS instance id")
	}
	if !*skipScrub {
		fmt.Fprintf(a.Stderr, "scrubbing source runner before AMI capture lease=%s instance=%s\n", leaseID, server.CloudID)
		if err := runSSHQuiet(ctx, target, remoteImageScrub()); err != nil {
			return exit(7, "image source scrub failed; rerun with --skip-scrub only if the box contains no secrets: %v", err)
		}
	}
	if coord, ok, err := newCoordinatorClient(cfg); err != nil {
		return err
	} else if ok {
		image, err := coord.CreateAWSImage(ctx, leaseID, *name, *description, *noReboot, *wait)
		if err != nil {
			return err
		}
		return printAWSImage(a.Stdout, image, *jsonOut)
	}
	client, err := newAWSClient(ctx, cfg)
	if err != nil {
		return err
	}
	image, err := client.CreateImage(ctx, cfg, server.CloudID, *name, *description, leaseID, serverSlug(server), *noReboot, *wait)
	if err != nil {
		return err
	}
	return printAWSImage(a.Stdout, image, *jsonOut)
}

func imageAWSConfig(provider string) (Config, error) {
	cfg, err := loadConfig()
	if err != nil {
		return Config{}, err
	}
	cfg.Provider = provider
	if cfg.Provider != "aws" {
		return Config{}, exit(2, "crabbox image only supports provider=aws")
	}
	return cfg, nil
}

func currentAWSImage(ctx context.Context, cfg Config) (AWSImage, error) {
	if coord, ok, err := newCoordinatorClient(cfg); err != nil {
		return AWSImage{}, err
	} else if ok {
		return coord.CurrentAWSImage(ctx, cfg.AWSRegion)
	}
	client, err := newAWSClient(ctx, cfg)
	if err != nil {
		return AWSImage{}, err
	}
	return client.CurrentImage(ctx, cfg)
}

func listAWSImages(ctx context.Context, cfg Config, name string) ([]AWSImage, error) {
	if coord, ok, err := newCoordinatorClient(cfg); err != nil {
		return nil, err
	} else if ok {
		return coord.AWSImages(ctx, cfg.AWSRegion, name)
	}
	client, err := newAWSClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return client.ListImages(ctx, cfg, name)
}

func printAWSImage(out interface{ Write([]byte) (int, error) }, image AWSImage, jsonOut bool) error {
	if jsonOut {
		return json.NewEncoder(out).Encode(image)
	}
	fmt.Fprintf(out, "id=%s\nname=%s\nstate=%s\nsource=%s\nregion=%s\ncreated=%s\n", image.ID, image.Name, image.State, blank(image.Source, "-"), blank(image.Region, "-"), blank(image.CreationDate, "-"))
	return nil
}

func remoteImageScrub() string {
	return `set -eu
sudo systemctl stop actions.runner.* 2>/dev/null || true
sudo rm -rf /root/.aws /root/.docker /home/*/.aws /home/*/.docker 2>/dev/null || true
sudo find /work/crabbox -path '*/.crabbox/actions/*.env.sh' -type f -delete 2>/dev/null || true
sudo find /work/crabbox -name '.env' -type f -delete 2>/dev/null || true
history -c 2>/dev/null || true
sudo rm -f /root/.*history /home/*/.*history 2>/dev/null || true
sudo cloud-init clean --logs 2>/dev/null || true
sudo journalctl --rotate 2>/dev/null || true
sudo journalctl --vacuum-time=1s 2>/dev/null || true
sync`
}
