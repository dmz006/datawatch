# Final gate

Read-only final gate for '{{feature}}'. Verify, with read and check only, that the slice is complete: the build is clean, the tests of the touched packages pass, and the touched files hang together ({{files_list}}). Write the verdict and the evidence to one report file, .datawatch/reports/{{feature}}-final-gate.txt (the feature slugged into the file name); that report is the only file this task writes. It makes no other edits and never runs a hosting CLI.
