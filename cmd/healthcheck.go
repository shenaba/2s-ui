package cmd

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/service"
)

// healthCheckTimeout is per dial. A container health check runs on a schedule
// and must not hang until the orchestrator's own timeout instead of answering:
// an unspecified listen address costs two dials, and both have to fit inside
// the Dockerfile's five-second HEALTHCHECK timeout.
const healthCheckTimeout = 2 * time.Second

// healthCheck reports whether the panel is accepting connections on the address
// it is actually configured with, and exits non-zero when it is not.
//
// The port comes from the database rather than the command line, because the
// operator can change it from the panel at any time. A check with the port baked
// into the Dockerfile goes red the moment they do, and the orchestrator then
// kills a container that was working perfectly.
//
// It dials rather than speaking HTTP: the panel serves plain HTTP or HTTPS
// depending on what is configured, and a check that assumed one would fail on
// the other. A listening socket is the question being asked.
func healthCheck() {
	// OpenDB, not InitDB. This runs every thirty seconds under the Dockerfile's
	// HEALTHCHECK, in a second process, against the database the panel is
	// writing to -- and InitDB is the start-up path: dedupStats scans the whole
	// stats table (a row per resource, tag, bucket and direction over a
	// thirty-day window, so millions on an active panel), AutoMigrate walks
	// fourteen models, and initUser would recreate admin/admin if it ever saw an
	// empty users table -- which ImportDB, swapping the file underneath, briefly
	// can. All of that to read two settings rows, under a five-second check
	// timeout that a slow scan blows through: three of those in a row and the
	// orchestrator kills a container that was serving fine.
	//
	// OpenDB alone installs the handle SettingService reads through, which is
	// the whole requirement here.
	if err := database.OpenDB(config.GetDBPath()); err != nil {
		fmt.Println("healthcheck: unable to open the database:", err)
		os.Exit(1)
	}

	settingService := service.SettingService{}
	port, err := settingService.GetPort()
	if err != nil {
		fmt.Println("healthcheck: unable to read the panel port:", err)
		os.Exit(1)
	}
	listen, err := settingService.GetListen()
	if err != nil {
		fmt.Println("healthcheck: unable to read the panel listen address:", err)
		os.Exit(1)
	}

	for _, host := range healthCheckHosts(listen) {
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		conn, dialErr := net.DialTimeout("tcp", addr, healthCheckTimeout)
		if dialErr == nil {
			conn.Close()
			fmt.Printf("healthcheck: panel is listening on %s\n", addr)
			return
		}
		err = dialErr
	}

	fmt.Printf("healthcheck: nothing listening on port %d: %v\n", port, err)
	os.Exit(1)
}

// healthCheckHosts is what to dial for a given webListen.
//
// An empty listen address means every interface, and the check runs beside the
// panel, so loopback is what it can reach -- both families, since a panel bound
// to "::" may have no IPv4 loopback socket at all. A configured address is
// dialled as given: loopback would not answer for it.
func healthCheckHosts(listen string) []string {
	if listen == "" || listen == "0.0.0.0" || listen == "::" {
		return []string{"127.0.0.1", "::1"}
	}
	return []string{listen}
}
