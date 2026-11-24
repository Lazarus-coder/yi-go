package game

import (
	_ "embed"
	"encoding/json"
)

//go:embed policy_weights.json
var policyWeights []byte

type policyUnit struct {
	weights   policyFeatures
	bias, out float32
}

func unpackPolicy(data []byte) []policyUnit {
	var packed struct {
		Hidden       [][]float32
		Bias, Output []float32
	}
	if err := json.Unmarshal(data, &packed); err != nil {
		panic(err)
	}
	if len(packed.Hidden) == 0 || len(packed.Hidden) != len(packed.Bias) || len(packed.Hidden) != len(packed.Output) {
		panic("invalid embedded Go policy dimensions")
	}
	units := make([]policyUnit, len(packed.Hidden))
	for i, row := range packed.Hidden {
		if len(row) != len(policyFeatures{}) {
			panic("embedded Go policy uses incompatible features")
		}
		units[i] = policyUnit{weights: policyFeatures(row), bias: packed.Bias[i], out: packed.Output[i]}
	}
	return units
}

var neuralPolicy = unpackPolicy(policyWeights)

func policyLogit(f policyFeatures) float64 {
	var logit float32
	for i := range neuralPolicy {
		unit := &neuralPolicy[i]
		z := unit.bias
		for j, feature := range f {
			z += feature * unit.weights[j]
		}
		if z > 0 {
			logit += unit.out * z
		}
	}
	return float64(logit)
}

func scoreNeuralMove(_ Move, features policyFeatures, heuristic float64) float64 {
	// The learned logit orders strategic alternatives; retain a small tactical
	// prior so easy captures and saves stay in the bounded candidate set.
	return policyLogit(features) + 0.005*max(-100, min(180, heuristic))
}
