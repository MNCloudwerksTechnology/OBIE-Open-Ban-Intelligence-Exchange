package simtrust

import (
	"fmt"
	"slices"
	"time"
)

// Fractions are the adversary fractions of the scenarios.
var Fractions = []float64{0.1, 0.2, 0.3, 0.4}

// Profiles are the settings profiles in report order.
var Profiles = []Profile{ProfileDefault, ProfileLab, ProfileAllowlist}

// worldStart is when every synthetic world starts, and the earliest start
// of a replayed trace: virtual time must lie in the future, because the
// store gives BadgerDB entries the expiry of their events, and BadgerDB
// hides an entry once the wall clock passes it.
var worldStart = time.Date(2100, 1, 4, 0, 0, 0, 0, time.UTC)

// Config is one configuration of a scenario.
type Config struct {
	Model    Model
	Fraction float64
	Profile  Profile
}

func (c Config) String() string {
	return fmt.Sprintf("%s at %.0f %% under %s", c.Model, 100*c.Fraction, c.Profile)
}

// Scenario is what `make sim-trust SCENARIO=…` runs: the configurations
// on the world, and the world's and the models' parameters (ADR 0034).
type Scenario struct {
	Name, Description string
	World             WorldParams
	Models            ModelParams
	Configs           []Config
}

// configsOf returns every configuration of models at fractions under
// profiles, honest-only once per profile.
func configsOf(models []Model, fractions []float64, profiles []Profile) []Config {
	var out []Config
	for _, p := range profiles {
		for _, m := range models {
			if m == ModelHonest {
				out = append(out, Config{Model: m, Profile: p})
				continue
			}
			for _, f := range fractions {
				out = append(out, Config{Model: m, Fraction: f, Profile: p})
			}
		}
	}
	return out
}

// Scenarios returns the scenarios: the baseline, one per behavior model,
// and the reduced one that CI runs.
func Scenarios() []Scenario {
	out := []Scenario{{
		Name:        "baseline",
		Description: "every behavior model at 10, 20, 30 and 40 % and honest-only, in the default, lab and allow-list settings",
		World:       DefaultWorld(),
		Models:      DefaultModels(),
		Configs:     configsOf(Models, Fractions, Profiles),
	}}
	for _, m := range Models {
		desc := fmt.Sprintf("the %s model at 10, 20, 30 and 40 %% in the default, lab and allow-list settings", m)
		if m == ModelHonest {
			desc = "honest publishers only, in the default, lab and allow-list settings"
		}
		out = append(out, Scenario{Name: string(m), Description: desc, World: DefaultWorld(), Models: DefaultModels(),
			Configs: configsOf([]Model{m}, Fractions, Profiles)})
	}
	w := DefaultWorld()
	w.Hours, w.Operators, w.AttackersPerHour = 36, 11, 10
	p := DefaultModels()
	p.JoinAt, p.DefectAt, p.Period = 6*time.Hour, 12*time.Hour, 12*time.Hour
	out = append(out, Scenario{
		Name:        "reduced",
		Description: "a 36-hour world of 11 operators at 10 attackers an hour; every behavior model at 20 and 40 % and honest-only, in the default and lab settings",
		World:       w,
		Models:      p,
		Configs:     configsOf(Models, []float64{0.2, 0.4}, []Profile{ProfileDefault, ProfileLab}),
	})
	return out
}

// ScenarioNamed returns the scenario called name.
func ScenarioNamed(name string) (Scenario, error) {
	all := Scenarios()
	if i := slices.IndexFunc(all, func(s Scenario) bool { return s.Name == name }); i >= 0 {
		return all[i], nil
	}
	var names []string
	for _, s := range all {
		names = append(names, s.Name)
	}
	return Scenario{}, fmt.Errorf("unknown scenario %q; want one of %q", name, names)
}
