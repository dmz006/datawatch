package session

import "os"

func writeFileForTest(path, body string) error { return os.WriteFile(path, []byte(body), 0o600) }
