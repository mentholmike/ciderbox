package cli

import "testing"

func TestValidateCoordinatorLeaseCapabilitiesRequiresDesktopEcho(t *testing.T) {
	err := validateCoordinatorLeaseCapabilities(Config{Desktop: true}, CoordinatorLease{ID: "cbx_test"})
	if err == nil {
		t.Fatal("expected desktop capability mismatch")
	}
}

func TestValidateCoordinatorLeaseCapabilitiesRequiresBrowserEcho(t *testing.T) {
	err := validateCoordinatorLeaseCapabilities(Config{Browser: true}, CoordinatorLease{ID: "cbx_test"})
	if err == nil {
		t.Fatal("expected browser capability mismatch")
	}
}

func TestValidateCoordinatorLeaseCapabilitiesRequiresCodeEcho(t *testing.T) {
	err := validateCoordinatorLeaseCapabilities(Config{Code: true}, CoordinatorLease{ID: "cbx_test"})
	if err == nil {
		t.Fatal("expected code capability mismatch")
	}
}

func TestValidateCoordinatorLeaseCapabilitiesAcceptsRequestedCapabilities(t *testing.T) {
	err := validateCoordinatorLeaseCapabilities(Config{Desktop: true, Browser: true, Code: true}, CoordinatorLease{
		ID:      "cbx_test",
		Desktop: true,
		Browser: true,
		Code:    true,
	})
	if err != nil {
		t.Fatalf("validateCoordinatorLeaseCapabilities error: %v", err)
	}
}

func TestEnforceManagedLeaseCapabilitiesAllowsMacOSScreenSharing(t *testing.T) {
	err := enforceManagedLeaseCapabilities(
		Config{Desktop: true},
		Server{Labels: map[string]string{"target": targetMacOS}},
		"cbx_test",
	)
	if err != nil {
		t.Fatalf("enforceManagedLeaseCapabilities error: %v", err)
	}
}
