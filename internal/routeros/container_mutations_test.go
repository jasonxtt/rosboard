package routeros

import (
	"context"
	"testing"
)

func TestContainerMutationsFailClosed(t *testing.T) {
	c := NewMutationClient("http://127.0.0.1:1", "test", "test")
	for _, tc := range []struct {
		m ContainerMenu
		c string
		f RouterOSFields
	}{
		{ContainerBridge, "set", RouterOSFields{"numbers": "*1", "vlan-filtering": "no"}},
		{ContainerList, "restart", RouterOSFields{"numbers": "*1"}},
		{ContainerList, "add", RouterOSFields{"check-certificate": "no"}},
		{ContainerList, "remove", RouterOSFields{"numbers": "*1", "name": "test"}},
		{ContainerList, "start", RouterOSFields{"numbers": "all"}},
		{ContainerVETH, "start", RouterOSFields{"numbers": "*1"}},
	} {
		if err := c.ContainerCommand(context.Background(), tc.m, tc.c, tc.f); err == nil {
			t.Fatal("unsafe mutation allowed", tc)
		}
	}
}

func TestContainerCPUListProjection(t *testing.T) {
	for input, want := range map[string]string{"": "", "0": "cpu0", "0,1-2": "cpu0,cpu1,cpu2", "cpu0,0": "cpu0"} {
		got, err := ContainerCPUList(input)
		if err != nil || got != want {
			t.Fatalf("%q => %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"-1", "256", "0-999999999", "2-1", "0,", "cpu0;evil"} {
		if _, err := ContainerCPUList(input); err == nil {
			t.Fatalf("invalid CPU accepted: %s", input)
		}
	}
}
