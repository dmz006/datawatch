package ebpfkver

import (
	"testing"

	"github.com/cilium/ebpf"
)

func TestParseRelease(t *testing.T) {
	cases := []struct {
		in   string
		want uint32
		bad  bool
	}{
		{"7.0.0-43-generic", 7 << 16, false},
		{"6.8.12", 6<<16 | 8<<8 | 12, false},
		{"5.10.300-foo", 5<<16 | 10<<8 | 255, false}, // patch clamps to 255
		{"6.8-rc1", 6<<16 | 8<<8, false},
		{"6.8", 6<<16 | 8<<8, false},
		{" 6.1.0\n", 6<<16 | 1<<8, false},
		{"", 0, true},
		{"garbage", 0, true},
		{"6", 0, true},
		{"6.x", 0, true},
		{"300.1.1", 0, true},
		{"4.300.1", 0, true},
	}
	for _, c := range cases {
		got, err := ParseRelease(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("ParseRelease(%q) = %#x, want error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("ParseRelease(%q) = %#x, %v; want %#x", c.in, got, err, c.want)
		}
	}
}

func TestSetKprobeVersions(t *testing.T) {
	spec := &ebpf.CollectionSpec{Programs: map[string]*ebpf.ProgramSpec{
		"k0":    {Type: ebpf.Kprobe},
		"kmag":  {Type: ebpf.Kprobe, KernelVersion: 0xFFFFFFFE},
		"kkeep": {Type: ebpf.Kprobe, KernelVersion: 0x00050a00},
		"other": {Type: ebpf.XDP},
	}}
	if err := SetKprobeVersions(spec, "6.8.12-generic"); err != nil {
		t.Fatal(err)
	}
	want := uint32(6<<16 | 8<<8 | 12)
	if got := spec.Programs["k0"].KernelVersion; got != want {
		t.Errorf("k0 = %#x want %#x", got, want)
	}
	if got := spec.Programs["kmag"].KernelVersion; got != want {
		t.Errorf("kmag = %#x want %#x", got, want)
	}
	if got := spec.Programs["kkeep"].KernelVersion; got != 0x00050a00 {
		t.Errorf("an explicit version must be kept, got %#x", got)
	}
	if got := spec.Programs["other"].KernelVersion; got != 0 {
		t.Errorf("non-kprobe must be untouched, got %#x", got)
	}
}

func TestSetKprobeVersions_BadReleaseLeavesSpecs(t *testing.T) {
	spec := &ebpf.CollectionSpec{Programs: map[string]*ebpf.ProgramSpec{"k0": {Type: ebpf.Kprobe}}}
	if err := SetKprobeVersions(spec, "nonsense"); err == nil {
		t.Fatal("want error")
	}
	if spec.Programs["k0"].KernelVersion != 0 {
		t.Error("spec changed despite error")
	}
}
