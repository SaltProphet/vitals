# Vitals

Portable live utilization monitor for **this PC**: CPU (per core), memory, disk, network, a named process tree, uptime, and thermal when Windows exposes a sensor.

No installer. Nothing is sent off the machine.

## Download (Windows 10/11, 64-bit)

**[Get Vitals 1.1.0](https://github.com/SaltProphet/vitals/releases/latest)**

1. Download `Vitals-1.1.0-windows-x64.zip`
2. Unzip anywhere
3. Double-click `Vitals.exe`
4. Leave it running while you watch the dashboard

If Windows says **Windows protected your PC**: More info → Run anyway. This build is not code-signed.

## What testers should receive

Send the release zip **or** this page. Do not send a source checkout or a workspace dump.

## Privacy

Vitals reads local system stats and serves the dashboard only on this computer. It does not phone home.

## Integrity (v1.1.0)

```
931b8f00b61af55cbf4809a0da68fdecb3c2fb41eb575e16639e6be7c3111e53  Vitals.exe
```

## Source

`desktop/` is the Windows app. Recipients do not need it to try the zip.
