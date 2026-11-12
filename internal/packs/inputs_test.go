package packs

import "testing"

func inputsPack() *Pack {
	return &Pack{Inputs: []Input{
		{Name: "region", Default: "us"},
		{Name: "token", Required: true},
	}}
}

func TestResolveInputsLevels(t *testing.T) {
	p := inputsPack()
	a := map[string]string{"region": "a", "token": "ta"}
	pr := map[string]string{"region": "p", "token": "tp"}
	g := map[string]string{"region": "g", "token": "tg"}

	got, err := ResolveInputs(p, a, pr, g)
	if err != nil || got["region"] != "a" || got["token"] != "ta" {
		t.Fatalf("automaton level: %v %v", got, err)
	}
	got, err = ResolveInputs(p, nil, pr, g)
	if err != nil || got["region"] != "p" || got["token"] != "tp" {
		t.Fatalf("project level: %v %v", got, err)
	}
	got, err = ResolveInputs(p, nil, nil, g)
	if err != nil || got["region"] != "g" || got["token"] != "tg" {
		t.Fatalf("global level: %v %v", got, err)
	}
}

func TestResolveInputsDefault(t *testing.T) {
	got, err := ResolveInputs(inputsPack(), nil, nil, map[string]string{"token": "t"})
	if err != nil || got["region"] != "us" {
		t.Fatalf("default: %v %v", got, err)
	}
}

func TestResolveInputsMissingRequired(t *testing.T) {
	_, err := ResolveInputs(inputsPack(), nil, nil, nil)
	want := "pack input token is required and has no value; set it at the automaton, project or global level"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
