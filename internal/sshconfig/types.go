package sshconfig

type Entry struct {
	Host           string
	Profile        string
	Source         string
	SourceID       string
	Notes          string
	PublicKey      string
	PrivateKey     string
	KeyFingerprint string
	Directives     map[string]string
}

func (e Entry) Clone() Entry {
	clone := e
	clone.Directives = map[string]string{}
	for key, value := range e.Directives {
		clone.Directives[key] = value
	}
	return clone
}
