# timesync.go

A simple tool to synchronize time without UDP (such as [NTP/NTS](https://en.wikipedia.org/wiki/Network_Time_Protocol)), which can resolve time mis-synchronization  in the school network behind the firewall

## Install

### Release

Download the executable file of your arch and OS in [Releases](../../releases/latest)

### Compile

Install Go (<https://go.dev>) >= 1.27 and `make` on your machine

Run the command following to build this tool for specified OS

```sh
# Windows
make build-win
# Linux
make build-linux
```

> If you want build for Windows 7 or older OS, check <https://github.com/thongtech/go-legacy-win7> to get the Windows7-compatible Go compiler

The output file is `bin/timesync-windows-amd64.exe` or `bin/timesync-linux-amd64`, or set `GOARCH=<arch>` to change the executable file arch

> [!NOTE]
> macOS is not supported

## Usage

Run as Administrator on Windows or as root on Linux to set system clock time, or write a config `config.json` in the working directory. If it is absent or invalid, the embedded default config will be used

## Config
Max size of config file is 6 MiB

### Example

> Comments is not allowed, the example config's comments are only used to describe the usage of fields

```jsonc
// config.json
{
    "log-level": "INFO",
    "user-agent": "...",
    "max-resp-size": 16384,  // 16 KiB
    "timeout": "2500ms",
    "retry": 2,  // total request attempts per endpoint, `0` as default `2` retries, `1` to disable retry
    "tls-verify": false,  // defaults to false for compatibility with broken TLS interception. Enable it whenever possible
    "endpoints": [
        {
            "url": "https://www.tsa.cn/api/time/getCurrentTime",  // url
            "type": "text",  // `text` / `json:<field>.<field>`, `..` as `.` / `regex:<regex>`
            "precision": "1ms",  // timestamp precision like `1ms` or `1s`, only in `ts` format
            "format": "ts",  // `ts` or `iso`
            "method": "get"  // HTTP Method
        },
        {
            "url": "https://sigelith.org/api/now/",
            "type": "json:utc",
            "format": "iso",
            "method": "get"
        },
        {
            "url": "https://aisenseapi.com/services/v1/timestamp",
            "type": "json:timestamp",
            "precision": "1s"
        }
    ]
}
```

## License

BSD 3-Clause License, see [LICENSE](./LICENSE)
