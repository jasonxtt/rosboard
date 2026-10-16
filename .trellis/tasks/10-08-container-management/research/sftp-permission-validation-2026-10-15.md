# Independent SFTP permission proof — 2026-10-15

The user asked which RouterOS permissions/services the planned host-side image
transfer needs. Run a disposable account/group/file matrix on the independent
lab only, using RouterOS 7.23.7 long-term. Validate the SSH host fingerprint
against the existing private pinned configuration. No production modifications
or runnable-code changes are part of this planning proof.

| Custom group policies | SFTP session | Result |
| --- | --- | --- |
| ssh | failed | SFTP subsystem could not open |
| ssh,read | failed | SFTP subsystem could not open |
| ssh,read,write | failed | SFTP subsystem could not open |
| ssh,read,ftp | opened | listing passed, directory creation denied |
| ssh,read,write,ftp | opened | list/mkdir/upload/read-back/rename/remove/rmdir passed |
| read,write,ftp | failed | SSH authentication denied |

The FTP service was disabled throughout; SSH was enabled on TCP/22. Therefore
`ftp` GROUP POLICY is needed for the tested SFTP workflow, but enabling the FTP
SERVICE is not. Use the verified ssh/read/write/ftp custom role for image transfer
and keep the existing REST control role separate. This proof establishes a
sufficient restricted policy combination, not every possible mathematical
minimum, and does not claim the result was tested on another RouterOS version.

The helper created only generated fixture groups/users with temporary random
credentials and one dedicated scratch namespace. A finally block removed all
created users/groups/files. Existing Nginx returned HTTP 200 afterward; no
production credentials, real private configuration or passwords are recorded
in this document or repository.

Required access path: application host -> configured RouterOS SSH port, permitted
by service source restrictions, account address restrictions and input firewall.
SFTP uses SSH; no additional SFTP service or FTP listener is needed. The app's
existing REST control still uses www or www-ssl. Group policy api/rest-api does
not imply a need to open the classic API services.

Primary references:
- SSH/SFTP service example:
  https://manual.mikrotik.com/docs/containers/user-guides/container-mosquitto-mqtt-server/
- SSH server:
  https://manual.mikrotik.com/docs/management-tools/ssh/
- User/group policy:
  https://help.mikrotik.com/docs/spaces/ROS/pages/8978504/User
- Management services:
  https://manual.mikrotik.com/docs/system-information-and-utilities/services/
