package model

type Variable struct {
	Name        string   `json:"name" yaml:"name"`
	Label       string   `json:"label" yaml:"label"`
	Type        string   `json:"type" yaml:"type"`
	Default     string   `json:"default,omitempty" yaml:"default,omitempty"`
	Placeholder string   `json:"placeholder,omitempty" yaml:"placeholder,omitempty"`
	Format      string   `json:"format,omitempty" yaml:"format,omitempty"`
	Required    bool     `json:"required,omitempty" yaml:"required,omitempty"`
	Options     []string `json:"options,omitempty" yaml:"options,omitempty"`
}

type Entry struct {
	ID          string     `json:"id" yaml:"id"`
	Trigger     string     `json:"trigger" yaml:"trigger"`
	Title       string     `json:"title" yaml:"title"`
	Description string     `json:"description,omitempty" yaml:"description,omitempty"`
	Category    string     `json:"category" yaml:"category"`
	Template    string     `json:"template" yaml:"template"`
	Variables   []Variable `json:"variables,omitempty" yaml:"variables,omitempty"`
	Tags        []string   `json:"tags,omitempty" yaml:"tags,omitempty"`
	Platform    string     `json:"platform,omitempty" yaml:"platform,omitempty"`
	Project     string     `json:"project,omitempty" yaml:"project,omitempty"`
	Environment string     `json:"environment,omitempty" yaml:"environment,omitempty"`
	RiskLevel   string     `json:"riskLevel" yaml:"risk_level"`
	Favorite    bool       `json:"favorite,omitempty" yaml:"favorite,omitempty"`
	UpdatedAt   string     `json:"updatedAt,omitempty" yaml:"updated_at,omitempty"`
	SourceFile  string     `json:"sourceFile,omitempty" yaml:"-"`
}

type Document struct {
	Version int     `json:"version" yaml:"version"`
	Entries []Entry `json:"entries" yaml:"entries"`
}
