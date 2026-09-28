package daemon

import (
	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
)

// consoleConfig returns cfg with the test hook Testing.ConsoleListen
// applied.
func consoleConfig(cfg config.Console, t Testing) config.Console {
	if t.ConsoleListen != "" {
		cfg.Listen = t.ConsoleListen
	}
	return cfg
}

// consoleService is the admin API's view of the web console.
type consoleService struct {
	console *console.Console
}

func (s consoleService) Console() admin.ConsoleResponse {
	return consoleResponse(s.console.State(), s.console.Token())
}

func (s consoleService) RotateConsoleToken() admin.ConsoleResponse {
	token := s.console.RotateToken()
	return consoleResponse(s.console.State(), token)
}

// consoleResponse converts the console's state into its admin API wire
// type.
func consoleResponse(st console.State, token string) admin.ConsoleResponse {
	resp := admin.ConsoleResponse{Enabled: st.Enabled, Listen: st.Listen, URL: st.URL, Token: token}
	if st.Err != nil {
		resp.Error = st.Err.Error()
	}
	return resp
}
