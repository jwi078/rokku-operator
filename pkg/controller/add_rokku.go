package controller

import (
	"github.com/jwi078/rokku-operator/pkg/controller/rokku"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, rokku.Add)
}
