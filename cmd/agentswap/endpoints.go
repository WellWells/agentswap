//go:build !mockapi

package main

import "github.com/WellWells/agentswap/internal/cli"

func endpoints() cli.Endpoints { return cli.Endpoints{} }
