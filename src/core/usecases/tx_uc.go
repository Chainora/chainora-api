package usecases

import "fmt"

type EIP712Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type EIP712Domain struct {
	Name              string `json:"name"`
	Version           string `json:"version"`
	ChainID           string `json:"chainId"`
	VerifyingContract string `json:"verifyingContract"`
}

type EIP712TypedData struct {
	Types       map[string][]EIP712Field `json:"types"`
	PrimaryType string                   `json:"primaryType"`
	Domain      EIP712Domain             `json:"domain"`
	Message     map[string]any           `json:"message"`
}

type BuildTransferTypedDataInput struct {
	ChainID           string
	VerifyingContract string
	From              string
	To                string
	Amount            string
	Nonce             string
}

type TxUsecase interface {
	BuildTransferTypedData(input BuildTransferTypedDataInput) (EIP712TypedData, error)
}

type txInteractor struct{}

func NewTxInteractor() TxUsecase {
	return &txInteractor{}
}

func (i *txInteractor) BuildTransferTypedData(input BuildTransferTypedDataInput) (EIP712TypedData, error) {
	if input.ChainID == "" || input.From == "" || input.To == "" || input.Amount == "" || input.Nonce == "" {
		return EIP712TypedData{}, fmt.Errorf("missing required transfer typed-data fields")
	}

	return EIP712TypedData{
		Types: map[string][]EIP712Field{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"TransferIntent": {
				{Name: "from", Type: "address"},
				{Name: "to", Type: "address"},
				{Name: "amount", Type: "uint256"},
				{Name: "nonce", Type: "string"},
			},
		},
		PrimaryType: "TransferIntent",
		Domain: EIP712Domain{
			Name:              "Chainora",
			Version:           "1",
			ChainID:           input.ChainID,
			VerifyingContract: input.VerifyingContract,
		},
		Message: map[string]any{
			"from":   input.From,
			"to":     input.To,
			"amount": input.Amount,
			"nonce":  input.Nonce,
		},
	}, nil
}
