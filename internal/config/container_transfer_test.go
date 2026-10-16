package config

import "testing"

func TestTransferCredentialSourceIsolation(t *testing.T) {
	d := DeviceConfig{RouterOS: RouterOSConfig{Username: "connection", Password: "connection-test"}, ContainerSSHHostKey: "SHA256:connection", ContainerSSHPort: 2222}
	c := d.ContainerTransferCredentials()
	if c.Username != "connection" || c.Password != "connection-test" || c.HostKey != "SHA256:connection" || c.Port != 2222 {
		t.Fatal("default credential reuse failed")
	}
	d.ContainerTransfer = &ContainerTransferConfig{Username: "dedicated", Password: "dedicated-test", HostKey: "SHA256:dedicated"}
	c = d.ContainerTransferCredentials()
	if c.Username != "dedicated" || c.Password != "dedicated-test" || c.HostKey != "SHA256:dedicated" || c.Port != 22 {
		t.Fatal("override source mixed")
	}
	d.ContainerTransfer = &ContainerTransferConfig{Username: "incomplete"}
	c = d.ContainerTransferCredentials()
	if c.Password != "" || c.HostKey != "" {
		t.Fatal("incomplete override inherited connection authority")
	}
	d.ContainerTransfer = nil
	if d.ContainerTransferCredentials().Username != "connection" {
		t.Fatal("switch-back failed")
	}
}
