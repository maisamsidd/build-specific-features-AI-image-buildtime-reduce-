package config

type Feature struct {
	Name      string   `yaml:"-"`
	Inputs    []string `yaml:"inputs"`
	Command   string   `yaml:"command"`
	DependsOn []string `yaml:"depends_on"`
}

type BuilderConfig struct {
	Features map[string]*Feature `yaml:"features"`
}
