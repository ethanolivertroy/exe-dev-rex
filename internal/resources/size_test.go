package resources

import "testing"

func TestSizeInputs(t *testing.T) {
	for _, s := range []string{"", "4", "4GB", "8G", "512MB", "512m", " 8 gb "} {
		if _, err := ParseSize(s); err != nil {
			t.Errorf("%q: %v", s, err)
		}
	}
	for _, s := range []string{"0", "0GB", "-1", "1.5GB", "8GiB", "lots", "999999999999999999999999GB"} {
		if _, err := ParseSize(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
	if n, _ := ParseSize("2GB"); n != 2048 {
		t.Fatalf("GB conversion: %d", n)
	}
	if n, _ := ParseSize("512MB"); n != 512 {
		t.Fatalf("MB conversion: %d", n)
	}
}

func TestCPUInputs(t *testing.T) {
	for _, s := range []string{"", "1", " 2 "} {
		if err := ValidateCPU(s); err != nil {
			t.Error(err)
		}
	}
	for _, s := range []string{"0", "-1", "1.5", "two", "+2"} {
		if ValidateCPU(s) == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
