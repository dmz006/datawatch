# signal-cli JSON-RPC Flow

The full JSON-RPC protocol between datawatch and signal-cli.

```mermaid
sequenceDiagram
    participant Go as datawatch (Go)
    participant Pipe as stdin/stdout pipe
    participant SCLI as signal-cli (Java)
    participant Signal as Signal Network

    Note over Go,SCLI: Daemon startup — subscribe to incoming messages
    Go->>Pipe: {"jsonrpc":"2.0","method":"subscribeReceive","id":1}\n
    Pipe->>SCLI: [reads line]
    SCLI->>Signal: open WebSocket to Signal servers
    SCLI->>Pipe: {"jsonrpc":"2.0","result":{},"id":1}\n
    Pipe->>Go: [reads line, resolves pending[1]]

    Note over Go,Signal: Incoming message arrives
    Signal->>SCLI: Signal protocol: data message from +12125551234
    SCLI->>Pipe: {"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+12125551234","dataMessage":{"message":"list","groupInfo":{"groupId":"base64=="}}}}}\n
    Pipe->>Go: [reads line, no id → notification → dispatchNotification]
    Go->>Go: filter self-messages, check group ID\nParse("list") → CmdList\nhandleList() → format reply

    Note over Go,SCLI: Send reply to group
    Go->>Pipe: {"jsonrpc":"2.0","method":"send","params":{"groupId":"base64==","message":"[myhost] Sessions:\n  [a3f2] running  write tests"},"id":2}\n
    Pipe->>SCLI: [reads line]
    SCLI->>Signal: Signal protocol: send group message
    SCLI->>Pipe: {"jsonrpc":"2.0","result":{"timestamp":1711234567890},"id":2}\n
    Pipe->>Go: [reads line, resolves pending[2] → Send() returns nil]

    Note over Go,SCLI: List groups
    Go->>Pipe: {"jsonrpc":"2.0","method":"listGroups","id":3}\n
    Pipe->>SCLI: [reads line]
    SCLI->>Pipe: {"jsonrpc":"2.0","result":[{"id":"base64==","name":"AI Control","active":true}],"id":3}\n
    Pipe->>Go: [reads line, resolves pending[3] → ListGroups() returns [{base64==, AI Control}]]
```
<sub>🔍 <a href="https://mermaid.live/view#pako:eNqdVU1v2zAM_SuCTinmbrXTj9VABwztEGToimI57BD7oMh0otWWPElOUQT576Ms20uaNu3ig2FRJPX4-CivKFcZ0Jga-FOD5HAj2FyzMpEEn4ppK7iomLRkpAgzJGOWPTLLF2QwUke7XveiAudnbCbkJ3yr2pIKjbuuk-vbceMq5pIVx7wQZPCdLdkLWSeNi3Nuv-7APir9kEjve6csELUEjSgDlzcmNwxKJREHpqkrktTRSXhKTD0zXIsZEKuIkFyVQs5JCcawORifbKSOv3xxdcRkldDfRkld8YTGCY0-niQ0SGgJdqGyxtQn_AkcxBKafeH2wnXSwXPJMKcHNtXAMkMKISH1287utpvSYqIqkOQXzCaKP4B1QNuiDWgs0WwF7cWpwdSFRdNq_TqqkdrCFBCMUsUSDEEc2MT5NEzTl4luAY-f8UiY1khFB9S3tyu_raXSyiquMNopqo_MtSrJhzAKo7OzszAanr6_2I2m6I1eoIxYaRwHCQW5hALp9Sujas2hCdg60UU5UD88Ju9c9ouEFsLYxmuuVV2NZa68j196CDNm4Pz06iqha_e8SbpEOWZOpeFlhAsrcsGZFajg1pYJU7m5u9vYSzcE6zLmorDYGgNFftxpOiB8AfyBNODI-AaR3DNtYNDVcdSdcF1mt86SyAWTWQFuMeh3c6VLZlEcVfG0Z-wmqBnv5ITbHHrIVGGWne69TG-w3Ztp-bRQxqYIxBikyMSJO3_KhnmUEl1L6YRKHrVA6BaMxeT9cESHjuyOqF0BLeUdugOmNqFWYLhlZeVG9yJ0-jw7v_h8ebIH89sDjUy0XXXtwh5rsLWWhkhRpHua6xThqzrkqnRyG_ng_pIc_i_j72NuumrzP1OKZKWXydcxuVbSalU0dsZtc1_EVtewTl9H9za3w57b277eDYanqw5QQP6BWKcpDZArHDGR4c94tcZlXeEtBN8yYZWmcc4KAwFltVWTJ8k91M6p_Wm3Xuu_vj6Riw">View this diagram fullscreen (zoom &amp; pan)</a></sub>

