# Interface Testing Tracker

This document tracks the validation status of all datawatch interfaces and communication channels.

Nothing has been validated yet.

| Interface             | Tested | Validated | Test Conditions | Notes                   |
|-----------------------|--------|-----------|-----------------|-------------------------|
| Signal                | No     | No        | —               | Not validated yet       |
| Telegram              | No     | No        | —               | Not validated yet       |
| Discord               | No     | No        | —               | Not validated yet       |
| Slack                 | No     | No        | —               | Not validated yet       |
| Matrix                | No     | No        | —               | Not validated yet       |
| Twilio SMS            | No     | No        | —               | Not validated yet       |
| ntfy                  | No     | No        | —               | Not validated yet       |
| Email (SMTP)          | No     | No        | —               | Not validated yet       |
| GitHub Webhook        | No     | No        | —               | Not validated yet       |
| Generic Webhook       | No     | No        | —               | Not validated yet       |
| Web UI                | No     | No        | —               | Not validated yet       |
| MCP stdio             | No     | No        | —               | Not validated yet       |
| MCP SSE               | No     | No        | —               | Not validated yet       |
| CLI                   | No     | No        | —               | Not validated yet       |

## Column Definitions

- **Tested**: The interface has been exercised in a real environment (not mocked)
- **Validated**: The interface behaves correctly end-to-end including error handling
- **Test Conditions**: OS/version, account type, network conditions, test scenario
- **Notes**: Observed issues, caveats, or special setup required

## How to Update

When an interface is tested, update the row with:
- Tested: Yes
- Validated: Yes (if working correctly) or No (if issues found)
- Test Conditions: e.g. "Linux 6.17, Telegram bot in private group, 2026-03-27"
- Notes: any relevant observations

Last updated: 2026-03-27
