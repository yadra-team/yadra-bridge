package version

var Version = "0.1.0"

func String() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
