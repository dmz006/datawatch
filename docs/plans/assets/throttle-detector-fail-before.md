# Throttle detector regression test, fail-before check

- `TestRateLimitDetectorIgnoresQuotedPhraseInThrottleTaskOutput`: exit status 1, does not compile on main (it references the new table)
- Copy of `TestRateLimitDetectorIgnoresQuotedPhraseInThrottleTaskOutput` written against the old generic list: exit status 1, fails on main
