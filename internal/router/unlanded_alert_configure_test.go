package router

import "testing"

func TestCheckUnlandedAlertValue(t *testing.T) {
	for _, bad := range [][2]string{{unlandedAlertEnabledKey, "maybe"}, {unlandedAlertThresholdKey, "-1"}, {unlandedAlertThresholdKey, "1.5"}, {unlandedAlertThresholdKey, "x"}} {
		if err := checkUnlandedAlertValue(bad[0], bad[1]); err == nil {
			t.Errorf("%s=%s accepted", bad[0], bad[1])
		}
	}
	for _, ok := range [][2]string{{unlandedAlertEnabledKey, "false"}, {unlandedAlertThresholdKey, "0"}, {unlandedAlertThresholdKey, "7"}} {
		if err := checkUnlandedAlertValue(ok[0], ok[1]); err != nil {
			t.Errorf("%s=%s refused: %v", ok[0], ok[1], err)
		}
	}
}

// A comm-channel configure write reaches the configure function (which is
// PUT /api/config in the daemon) only for a valid value.
func TestConfigureWriteUnlandedAlert(t *testing.T) {
	var wrote []string
	r := &Router{hostname: "h"}
	r.SetConfigureFunc(func(key, value string) error {
		wrote = append(wrote, key+"="+value)
		return nil
	})
	if err := r.configureWrite(unlandedAlertThresholdKey, "-2"); err == nil {
		t.Fatal("negative threshold accepted")
	}
	if err := r.configureWrite(unlandedAlertEnabledKey, "nope"); err == nil {
		t.Fatal("non-boolean accepted")
	}
	if len(wrote) != 0 {
		t.Fatalf("refused value written: %v", wrote)
	}
	if err := r.configureWrite(unlandedAlertThresholdKey, "5"); err != nil {
		t.Fatal(err)
	}
	if err := r.configureWrite(unlandedAlertEnabledKey, "false"); err != nil {
		t.Fatal(err)
	}
	if len(wrote) != 2 {
		t.Fatalf("wrote = %v", wrote)
	}
}
