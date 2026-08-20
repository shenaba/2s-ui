package cmd

import (
	"fmt"

	"github.com/shenaba/2s-ui/config"
	"github.com/shenaba/2s-ui/database"
	"github.com/shenaba/2s-ui/service"
)

func resetAdmin() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}

	userService := service.UserService{}
	err = userService.UpdateFirstUser("admin", "admin")
	if err != nil {
		fmt.Println("reset admin credentials failed:", err)
	} else {
		fmt.Println("reset admin credentials success")
	}
}

// disableAdminTwoFa is the only way back in once the second factor cannot be
// satisfied -- a lost authenticator, or a clock that moved backwards past the
// replay high-water mark. The panel side of this asks for the password on
// purpose; here the password is not what is missing.
func disableAdminTwoFa() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}

	userService := service.UserService{}
	err = userService.ClearFirstUserTwoFa()
	if err != nil {
		fmt.Println("disable two-factor authentication failed:", err)
	} else {
		fmt.Println("two-factor authentication disabled")
	}
}

// unlockAdminLogin is the out-of-band recovery path for the login limiter.
// Exposing this through the panel would let an unauthenticated caller erase
// the protection it is meant to enforce; shell access already implies access
// to the database and is the same trust boundary as credential/2FA recovery.
func unlockAdminLogin() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}

	guard := service.LoginGuardService{}
	if err := guard.ClearAll(); err != nil {
		fmt.Println("clear login bans failed:", err)
	} else {
		fmt.Println("login rate-limit counts and bans cleared")
	}
}

func updateAdmin(username string, password string) {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}

	if username != "" || password != "" {
		userService := service.UserService{}
		err := userService.UpdateFirstUser(username, password)
		if err != nil {
			fmt.Println("reset admin credentials failed:", err)
		} else {
			fmt.Println("reset admin credentials success")
		}
	}
}

func showAdmin() {
	err := database.InitDB(config.GetDBPath())
	if err != nil {
		fmt.Println(err)
		return
	}
	userService := service.UserService{}
	userModel, err := userService.GetFirstUser()
	if err != nil {
		fmt.Println("get current user info failed,error info:", err)
	}
	username := userModel.Username
	userpasswd := userModel.Password
	if (username == "") || (userpasswd == "") {
		fmt.Println("current username or password is empty")
	}
	fmt.Println("First admin credentials:")
	fmt.Println("\tUsername:\t", username)
	fmt.Println("\tPassword:\t <hashed, not recoverable>")
	fmt.Println("To set a new password, run: s-ui admin -username <user> -password <pass>")
}
