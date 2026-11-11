package atime

import "testing"

func TestPolicyMayUpdate(t *testing.T) {
	for p, want := range map[Policy]bool{
		PolicyNone:    false,
		PolicyLazy:    true,
		PolicyStrict:  true,
		PolicyUnknown: true,
	} {
		if got := p.MayUpdate(); got != want {
			t.Errorf("%q.MayUpdate() = %v, want %v", p, got, want)
		}
	}
}

func TestAnyMayUpdate(t *testing.T) {
	if AnyMayUpdate(nil) {
		t.Error("AnyMayUpdate(nil) = true, want false")
	}
	if AnyMayUpdate([]Volume{{Policy: PolicyNone}, {Policy: PolicyNone}}) {
		t.Error("AnyMayUpdate(all none) = true, want false")
	}
	if !AnyMayUpdate([]Volume{{Policy: PolicyNone}, {Policy: PolicyLazy}}) {
		t.Error("AnyMayUpdate(one lazy) = false, want true")
	}
}

func TestShouldObserve(t *testing.T) {
	if !ShouldObserve(nil) {
		t.Error("ShouldObserve(nil) = false, want true: unprobed volumes are treated as unknown")
	}
	if ShouldObserve([]Volume{{Policy: PolicyNone}}) {
		t.Error("ShouldObserve(read-only) = true, want false")
	}
	if !ShouldObserve([]Volume{{Policy: PolicyNone}, {Policy: PolicyLazy}}) {
		t.Error("ShouldObserve(one lazy) = false, want true")
	}
}
