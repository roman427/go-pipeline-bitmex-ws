package main

import (
	"flag"
	"fmt"
	"os"

	"app"
)

func main() {
	fmt.Println("Start pipelines controler")
	// get args
	var configpath string
	argconfigpath := flag.String("path", "", "a string")
	flag.Parse()
	if *argconfigpath == "" {
		mainpath, _ := os.Getwd()
		configpath = fmt.Sprintf("%s/%s", mainpath, ".conf/main.yaml")
	} else {
		configpath = *argconfigpath
	}

	eng := app.NewEngine(configpath)

	pgxpool, err := app.InitPgxDriver(eng.GetContext(), eng.GetDburl())
	if err != nil {
		fmt.Println("Pgx driver: Can't connect to database", err)
		os.Exit(1)
	}
	eng.SetPgxDB(pgxpool)

	if err := eng.Run(); err != nil {
		fmt.Println("Engine start error:", err)
		os.Exit(1)
	}

	<-eng.Complete()
}
