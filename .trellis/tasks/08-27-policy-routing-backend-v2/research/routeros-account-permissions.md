# RouterOS device account permissions

Official MikroTik documentation defines `read` as configuration read access, `write` as ordinary configuration write access excluding user management, `policy` as user-management access, and `rest-api` as REST API access.

Decision:

- rosboard's unified managed device account uses `read,write,test,api,rest-api`.
- It does not receive `policy`, so policy routing can mutate routes/firewall/DNS without granting RouterOS user-management rights.
- Existing managed accounts created by the old script are detected as read-only and users are directed to replace the device account.

Primary references:

- https://help.mikrotik.com/docs/spaces/ROS/pages/8978504/User
- https://help.mikrotik.com/docs/spaces/ROS/pages/47579162/REST+API
