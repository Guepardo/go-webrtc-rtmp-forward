package controllers

import (
	"encoding/json"
	"net/http"
)

type BaseController struct {
}

func (base BaseController) renderJson(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
