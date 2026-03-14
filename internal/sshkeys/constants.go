package sshkeys

var BASE_URL_V1 string = "/v1/resource/ssh-keys/"

// SupportedAlgorithms lists the known SSH key generation algorithms.
// The actual supported list is verified at runtime via CheckAlgorithms.
var SupportedAlgorithms = []string{"ecdsa-p256", "ed25519", "rsa-2048", "rsa-4096"}
