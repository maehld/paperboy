[← Paperboy](../README.md)

# OpenDisplay

Delivery to OpenDisplay devices is currently **not available**: the image-upload pipeline it relied on was removed, and plans for OpenDisplay are a TODO. The protocol implementation (`internal/opendisplay`, including Bluetooth) and the test program below remain in the repository.

## Test program

```powershell
# Read firmware and panel configuration
go run ./cmd/odtest -host 192.168.1.50
# Additionally send a test image (optional -fast, -png preview.png)
go run ./cmd/odtest -host 192.168.1.50 -send
# Via Bluetooth: scan for devices, then as above by name or address
go run ./cmd/odtest -scan
go run ./cmd/odtest -ble ODE46FD8 [-send] [-buttons]
```

Bluetooth (ADR 0016) exists only in the test program. On a reTerminal Sticky under Windows a full image (48 KB) takes about 30 s, via Wi-Fi a few seconds. After disconnecting, the display takes a few seconds until it is found again (if necessary `-scan-time 20s`).
