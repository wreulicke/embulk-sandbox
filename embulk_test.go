package main

import (
	"context"
	"database/sql"
	"io"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

var mysqlWaitFor = wait.ForLog("port: 3306  MySQL Community Server - GPL")
var ne, err = network.New(context.Background())

func StartMySQL(t *testing.T, cus ...testcontainers.ContainerCustomizer) testcontainers.Container {
	t.Helper()

	r := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "mysql:8.4",
			ExposedPorts: []string{"3306/tcp"},
			Env: map[string]string{
				"MYSQL_DATABASE":             "app",
				"MYSQL_USER":                 "user",
				"MYSQL_PASSWORD":             "password",
				"MYSQL_ALLOW_EMPTY_PASSWORD": "yes",
			},
			Networks: []string{ne.Name},
			Files: []testcontainers.ContainerFile{
				{
					ContainerFilePath: "/etc/mysql/conf.d/mysql.cnf",
					HostFilePath:      "mysql.cnf",
					FileMode:          0644,
				},
				{
					ContainerFilePath: "/docker-entrypoint-initdb.d/init.sql",
					HostFilePath:      "init.sql",
					FileMode:          0644,
				},
			},
		},
		Logger: testcontainers.TestLogger(t),
	}

	for _, c := range cus {
		if err := c.Customize(&r); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()
	mysql, err := testcontainers.GenericContainer(ctx, r)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		err := mysql.Terminate(ctx)
		if err != nil {
			t.Fatal(err)
		}
	})

	return mysql
}

func getMySQLHostAndPort(t *testing.T, mysql testcontainers.Container) (string, nat.Port) {
	t.Helper()
	host, err := mysql.Host(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	port, err := mysql.MappedPort(context.Background(), "3306/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

func TestPing(t *testing.T) {
	mysqlContainer := StartMySQL(t)
	err := mysqlContainer.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mysqlWaitFor.WaitUntilReady(context.Background(), mysqlContainer)

	host, port := getMySQLHostAndPort(t, mysqlContainer)

	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(host, port.Port())
	cfg.DBName = "app"
	cfg.User = "user"
	cfg.Passwd = "password"

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()

	err = db.Ping()
	if err != nil {
		t.Fatal(err)
	}
}

type mysqlCon struct {
	Host     string
	Port     string
	Database string
	User     string
	Password string
}

func dockerBuild(t *testing.T) {
	cmd := exec.Command("docker", "build", "-t", "embulk-sandbox:latest", ".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		t.Fatal(err)
	}
}

func ExecuteMySQLToMySQL(t *testing.T, configPath string, src mysqlCon, dst mysqlCon) testcontainers.Container {
	t.Helper()
	dockerBuild(t)

	env := map[string]string{}
	env["SRC_HOST"] = src.Host
	env["SRC_PORT"] = src.Port

	env["DST_HOST"] = dst.Host
	env["DST_PORT"] = dst.Port

	r := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "embulk-sandbox:latest",
			Env:   env,
			Files: []testcontainers.ContainerFile{
				{
					ContainerFilePath: "/app/config.yml.liquid",
					HostFilePath:      configPath,
					FileMode:          0644,
				},
			},
			Networks: []string{ne.Name},
			Cmd: []string{
				"/app/config.yml.liquid",
			},
		},
		Logger: testcontainers.TestLogger(t),
	}

	ctx := context.Background()
	embulk, err := testcontainers.GenericContainer(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := embulk.Terminate(ctx)
		if err != nil {
			t.Fatal(err)
		}
	})
	return embulk
}

func TestEmbulk(t *testing.T) {
	srcMySQL := StartMySQL(t, testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
		req.NetworkAliases = map[string][]string{
			ne.Name: {"src"},
		}
		return nil
	}))
	dstMySQL := StartMySQL(t, testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
		req.NetworkAliases = map[string][]string{
			ne.Name: {"dst"},
		}
		return nil
	}))

	start := func(c testcontainers.Container) {
		err := c.Start(context.Background())
		if err != nil {
			t.Error(err)
		}
	}
	go start(srcMySQL)
	go start(dstMySQL)
	err := mysqlWaitFor.WaitUntilReady(context.Background(), srcMySQL)
	if err != nil {
		r, _ := srcMySQL.Logs(context.Background())
		_, _ = io.Copy(os.Stdout, r)
		t.Fatal(err)
	}

	err = mysqlWaitFor.WaitUntilReady(context.Background(), dstMySQL)
	if err != nil {
		r, _ := dstMySQL.Logs(context.Background())
		_, _ = io.Copy(os.Stdout, r)
		t.Fatal(err)
	}

	// _, srcPort := getMySQLHostAndPort(t, srcMySQL)
	// _, dstPort := getMySQLHostAndPort(t, dstMySQL)

	configPath := "mysql_to_mysql.yaml"

	c := ExecuteMySQLToMySQL(t, configPath, mysqlCon{
		Host: "src",
		Port: "3306",
	}, mysqlCon{
		Host: "dst",
		Port: "3306",
	})

	t.Log("start embulk")
	err = c.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	for {
		select {
		case <-tick.C:
			if !c.IsRunning() {
				return
			}
			i, _ := c.Inspect(context.Background())

			if i.State.Status != "running" {
				if i.State.ExitCode != 0 {
					t.Error("embulk exited with code", i.State.ExitCode)
					logs, err := c.Logs(context.Background())
					if err != nil {
						t.Error(err)
					}
					_, _ = io.Copy(os.Stdout, logs)
				}
				return
			}
		case <-ctx.Done():
			t.Error("timeout")
			return
		}
	}
}
