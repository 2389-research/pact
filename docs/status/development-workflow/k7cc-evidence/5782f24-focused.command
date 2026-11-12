env -u GOROOT mise exec -- go test ./cmd/pact ./tests/e2e -run 'TestQuickstart|TestCLIDevelopmentGuideExample' -count=1 -v
