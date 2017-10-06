//UFA TOOL V2 HLF V0.6
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hyperledger/fabric/core/chaincode/shim"
	"github.com/hyperledger/fabric/core/crypto/primitives"
)

var logger = shim.NewLogger("UFAChainCode")

//ALL_ELEMENENTS Key to refer the master list of UFA
const ALL_ELEMENENTS = "ALL_RECS"

//ALL_INVOICES key to refer the invoice master data
const ALL_INVOICES = "ALL_INVOICES"

//UFA_TRXN_PREFIX Key prefix for UFA transaction history
const UFA_TRXN_PREFIX = "UFA_TRXN_HISTORY_"

//UFA_INVOICE_PREFIX Key prefix for identifying Invoices assciated with a ufa
const UFA_INVOICE_PREFIX = "UFA_INVOICE_PREFIX_"

const CHAIN_CODE_VERSION = "CHAIN_CODE_VERSION"

//UFAChainCode Chaincode default interface
type UFAChainCode struct {
}

//Update invoices
func updateInvoices(stub shim.ChaincodeStubInterface, args []string) ([]byte, error) {
	var inputData []map[string]interface{}
	var existingRecMap map[string]interface{}

	logger.Info("updateInvoices called ")

	//TODO: Update the validation here
	//who := args[0]
	payload := args[1]
	logger.Info("updateInvoices payload passed " + payload)

	//who :=args[2]
	json.Unmarshal([]byte(payload), &inputData)
	for _, invoiceDataFields := range inputData {
		debugBytes, _ := json.Marshal(invoiceDataFields)
		logger.Info("updateInvoices payload passed " + string(debugBytes))
		invoiceNumber := getSafeString(invoiceDataFields["invoiceNumber"])
		logger.Info("updateInvoices going to get details of invoice " + invoiceNumber)

		recBytes, _ := stub.GetState(invoiceNumber)
		json.Unmarshal(recBytes, &existingRecMap)
		updatedReord, _ := updateFields(existingRecMap, invoiceDataFields)
		updatedRecJSON, _ := json.Marshal(updatedReord)
		stub.PutState(invoiceNumber, updatedRecJSON)
	}

	return nil, nil
}
func getAllInvoicesForUsr(stub shim.ChaincodeStubInterface, args []string) ([]byte, error) {
	logger.Info("getAllInvoicesForUsr called")
	who := args[0]

	recordsList, err := getAllInvloiceFromMasterList(stub)
	if err != nil {
		return nil, errors.New("Unable to get all the inventory records ")
	}
	var outputRecords []map[string]interface{}
	outputRecords = make([]map[string]interface{}, 0)
	for _, invoiceNumber := range recordsList {
		logger.Info("getAllInvoicesForUsr: Processing inventory record " + invoiceNumber)
		recBytes, _ := stub.GetState(invoiceNumber)
		var record map[string]interface{}
		json.Unmarshal(recBytes, &record)
		if record["approvedBy"] == who || record["raisedBy"] == who {
			outputRecords = append(outputRecords, record)
		}
	}
	outputBytes, _ := json.Marshal(outputRecords)
	logger.Info("Returning records from getAllInvoicesForUsr " + string(outputBytes))
	return outputBytes, nil
}

//Returns all the Invoice created so far for the interest parties
func getAllNonExpiredUFA(stub shim.ChaincodeStubInterface, args []string) ([]byte, error) {
	logger.Info("getAllNonExiredUFA called")
	who := args[0]

	recordsList, err := getAllRecordsList(stub)
	if err != nil {
		return nil, errors.New("Unable to get all the UFA records records ")
	}
	var outputRecords []map[string]interface{}
	outputRecords = make([]map[string]interface{}, 0)
	for _, ufanumber := range recordsList {
		logger.Info("getAllNonExpiredUFA: Processing UFA for " + ufanumber)
		recBytes, _ := stub.GetState(ufanumber)
		var ufaRecord map[string]interface{}
		json.Unmarshal(recBytes, &ufaRecord)

		if (ufaRecord["sellerApprover"].(map[string]interface{})["emailid"] == who || ufaRecord["buyerApprover"].(map[string]interface{})["emailid"] == who) && ufaRecord["status"].(string) == "Agreed" && !isUFAExpired(ufaRecord) {
			outputRecords = append(outputRecords, ufaRecord)
		}
	}
	outputBytes, _ := json.Marshal(outputRecords)
	logger.Info("Returning records from getAllNonExiredUFA " + string(outputBytes))
	return outputBytes, nil
}

//Checks if UFA amounts are exhausted or not
func isUFAExpired(ufaDetails map[string]interface{}) bool {
	if ufaDetails != nil {
		raisedTotal := getSafeNumber(ufaDetails["raisedInvTotal"])
		totalCharge := getSafeNumber(ufaDetails["netCharge"])
		tolerance := getSafeNumber(ufaDetails["chargTolrence"])
		maxCharge := totalCharge + (totalCharge * tolerance / 100)
		return !(raisedTotal < maxCharge)
	}
	return true
}

//Retrieve all the invoice list
func getAllInvloiceFromMasterList(stub shim.ChaincodeStubInterface) ([]string, error) {
	var recordList []string
	recBytes, _ := stub.GetState(ALL_INVOICES)

	err := json.Unmarshal(recBytes, &recordList)
	if err != nil {
		return nil, errors.New("Failed to unmarshal getAllInvloiceFromMasterList ")
	}

	return recordList, nil
}

//Create new invoices and update UFA details
func createInvoices(stub shim.ChaincodeStubInterface, args []string) ([]byte, error) {
	//Validate the invoice entry
	var invoices []map[string]interface{}
	var ufaDetails map[string]interface{}
	var invoiceNumberList bytes.Buffer
	var invoiceNumbers []string
	logger.Info("Inside createInvoices")

	errorMessages := validateInvoiceDetails(stub, args)
	if errorMessages == "" {
		payload := args[1]
		logger.Info("Inside createInvoices: Payload received " + payload)
		json.Unmarshal([]byte(payload), &invoices)
		//Since this is validated so no more validation
		firstInvoice := invoices[0]
		ufanumber := getSafeString(firstInvoice["ufanumber"])
		ufaBytes, _ := stub.GetState(ufanumber)
		json.Unmarshal(ufaBytes, &ufaDetails)
		//Collect period
		billingPeriod := getSafeString(firstInvoice["billingPeriod"])
		totalAmt := 0.0
		invoiceNumbers = make([]string, len(invoices))
		//Collect invoice numbers and sum of values
		for _, invoice := range invoices {
			totalAmt = totalAmt + getSafeNumber(invoice["invoiceAmt"])
			invNumber := getSafeString(invoice["invoiceNumber"])
			invoiceNumberList.WriteString(invNumber)
			invoiceNumberList.WriteString(",")
			invoiceNumbers = append(invoiceNumbers, invNumber)
			//Perist the invoices while collecting the details
			invoiceJSON, _ := json.Marshal(invoice)
			logger.Info("Persisting invoice :" + string(invoiceJSON))
			stub.PutState(invNumber, invoiceJSON)

		}

		attrName := "invperiod_" + billingPeriod
		ufaDetails[attrName] = invoiceNumberList.String()
		chargesSoFar := getSafeNumber(ufaDetails["raisedInvTotal"])
		ufaDetails["raisedInvTotal"] = strconv.FormatFloat(chargesSoFar+totalAmt/2.0, 'f', -1, 64)

		//Update the gloval invoice list
		updateInvoiceMasterRecords(stub, invoiceNumbers)
		//Update the running total
		//Update the invoice numbers list
		existingInvoiceList := getSafeString(ufaDetails["allInvoiceList"])
		newListOfInvoices := existingInvoiceList + invoiceNumberList.String()
		ufaDetails["allInvoiceList"] = newListOfInvoices
		updatedUfaBytes, _ := json.Marshal(ufaDetails)
		logger.Info("UFA record after invoice related updation " + string(updatedUfaBytes))
		//Update the UFA
		stub.PutState(ufanumber, updatedUfaBytes)
		//Update the trxn history of UFA
		appendUFATransactionHistory(stub, ufanumber, string(updatedUfaBytes))
		logger.Info("UFA update completed")
		return nil, nil
	}
	//Validation issue
	output := "{\"validation\":\"Failure\",\"msg\" : \"" + errorMessages + "\" }"
	return []byte(output), nil

}

//Update master invoice list
func updateInvoiceMasterRecords(stub shim.ChaincodeStubInterface, invoiceList []string) error {
	var recordList []string
	recBytes, _ := stub.GetState(ALL_INVOICES)

	err := json.Unmarshal(recBytes, &recordList)
	if err != nil {
		return errors.New("Failed to unmarshal updateInvoiceMasterRecords ")
	}
	recordList = append(recordList, invoiceList...)
	bytesToStore, _ := json.Marshal(recordList)
	logger.Info("After addition" + string(bytesToStore))
	stub.PutState(ALL_INVOICES, bytesToStore)
	return nil
}

//Returns all the invoices raised for an UFA
func getInvoicesForUFA(stub shim.ChaincodeStubInterface, args []string) []byte {
	logger.Info("getInvoicesForUFA called")
	var ufadetails map[string]interface{}
	var outputRecords []map[string]interface{}
	outputRecords = make([]map[string]interface{}, 0)
	ufanumber := args[1]
	ufaBytes, _ := stub.GetState(ufanumber)
	json.Unmarshal(ufaBytes, &ufadetails)
	if ufadetails["allInvoiceList"] != nil {
		recordsList := strings.Split(getSafeString(ufadetails["allInvoiceList"]), ",")
		for _, invoiceNumber := range recordsList {
			logger.Info("getInvoicesForUFA: Processing record " + invoiceNumber)
			if len(invoiceNumber) > 0 {
				recBytes, _ := stub.GetState(invoiceNumber)
				var record map[string]interface{}
				json.Unmarshal(recBytes, &record)
				outputRecords = append(outputRecords, record)
			}
		}

	}

	outputJSON, _ := json.Marshal(outputRecords)
	logger.Info("Returning records from getInvoicesForUFA ")
	return outputJSON
}

//Validate the new Invoice created
func validateNewInvoideData(stub shim.ChaincodeStubInterface, args []string) []byte {
	var output string
	msg := validateInvoiceDetails(stub, args)

	if msg == "" {
		output = "{\"validation\":\"Success\",\"msg\" : \"\" }"
	} else {
		output = "{\"validation\":\"Failure\",\"msg\" : \"" + msg + "\" }"
	}
	return []byte(output)
}

//Validate the new invoice payload
func validateInvoiceDetails(stub shim.ChaincodeStubInterface, args []string) string {
	var output string
	var errorMessages []string
	var invoices []map[string]interface{}
	var ufaDetails map[string]interface{}
	//I am assuming the invoices would sent as an array and must be multiple
	payload := args[1]
	json.Unmarshal([]byte(payload), &invoices)
	if len(invoices) < 2 {
		errorMessages = append(errorMessages, "Invalid number of invoices")
	} else {
		//Now checking the ufa number
		firstInvoice := invoices[0]
		ufanumber := getSafeString(firstInvoice["ufanumber"])
		if ufanumber == "" {
			errorMessages = append(errorMessages, "UFA number not provided")
		} else {
			recBytes, err := stub.GetState(ufanumber)
			if err != nil || recBytes == nil {
				errorMessages = append(errorMessages, "Invalid UFA number provided")
			} else {
				json.Unmarshal(recBytes, &ufaDetails)
				//Rasied invoice shoul not be exhausted
				raisedTotal := getSafeNumber(ufaDetails["raisedInvTotal"])
				totalCharge := getSafeNumber(ufaDetails["netCharge"])
				tolerance := getSafeNumber(ufaDetails["chargTolrence"])
				maxCharge := totalCharge + (totalCharge * tolerance / 100)
				if raisedTotal == maxCharge {
					errorMessages = append(errorMessages, "All charges exhausted. Invoices can not raised")
				}
				//Now check if invoice is already raised for the period or not
				billingPerid := getSafeString(firstInvoice["billingPeriod"])
				if billingPerid == "" {
					errorMessages = append(errorMessages, "Invalid billing period")
				}
				if ufaDetails["invperiod_"+billingPerid] != nil {
					errorMessages = append(errorMessages, "Invoice already raised for the month")
				}
				//Now check the sum of invoice amount
				runningTotal := 0.0
				var buffer bytes.Buffer
				for _, invoice := range invoices {
					invoiceNumber := getSafeString(invoice["invoiceNumber"])
					amount := getSafeNumber(invoice["invoiceAmt"])
					if amount < 0 {
						errorMessages = append(errorMessages, "Invalid invoice amount in "+invoiceNumber)
						break
					}
					buffer.WriteString(invoiceNumber)
					buffer.WriteString(",")
					runningTotal = runningTotal + amount
				}
				if (raisedTotal + runningTotal/2) >= maxCharge {
					errorMessages = append(errorMessages, "Invoice value is exceeding total allowed charge")
				}

			}

		}

	}
	if len(errorMessages) == 0 {
		output = ""
	} else {
		outputBytes, _ := json.Marshal(errorMessages)
		output = string(outputBytes)
	}
	return output
}

// Update and existing UFA record
func updateUFA(stub shim.ChaincodeStubInterface, args []string) ([]byte, error) {
	var existingRecMap map[string]interface{}
	var updatedFields map[string]interface{}

	logger.Info("updateUFA called ")

	ufanumber := args[0]
	//TODO: Update the validation here
	//who := args[1]
	payload := args[2]
	logger.Info("updateUFA payload passed " + payload)

	//who :=args[2]
	recBytes, _ := stub.GetState(ufanumber)

	json.Unmarshal(recBytes, &existingRecMap)
	json.Unmarshal([]byte(payload), &updatedFields)
	updatedReord, _ := updateFields(existingRecMap, updatedFields)
	outputMapBytes, _ := json.Marshal(updatedReord)
	logger.Info("updateUFA: Final json after update " + string(outputMapBytes))
	//Store the records
	stub.PutState(ufanumber, outputMapBytes)
	appendUFATransactionHistory(stub, ufanumber, payload)
	return nil, nil
}

//Updating the fileds in a generic way
func updateFields(existingRecordMap map[string]interface{}, modifiedRecordMap map[string]interface{}) (map[string]interface{}, error) {
	logger.Info("UpdateFields called ")
	for k, v := range modifiedRecordMap {
		logger.Info(" Parsing the key from modifiedRecordMap" + k)
		switch v.(type) {
		case string:
			existingRecordMap[k] = v
		case int:
			existingRecordMap[k] = v
		case []interface{}:
			//Just replcaing the old one with the new one
			existingRecordMap[k] = modifiedRecordMap[k]
		case interface{}:
			if existingRecordMap[k] == nil {
				//The entry in the modified filed does not exist
				existingRecordMap[k] = modifiedRecordMap[k]
			} else {
				record := existingRecordMap[k].(map[string]interface{})
				modFields := modifiedRecordMap[k].(map[string]interface{})
				existingRecordMap[k], _ = updateFields(record, modFields)
			}
		default:
			logger.Info(" Field not recognized " + k)
		}
	}
	return existingRecordMap, nil
}

//Get a single ufa
func getUFADetails(stub shim.ChaincodeStubInterface, args []string) ([]byte, error) {
	logger.Info("getUFADetails called with UFA number: " + args[0])

	var outputRecord map[string]interface{}
	ufanumber := args[0] //UFA ufanum
	//who :=args[1] //Role
	recBytes, _ := stub.GetState(ufanumber)
	json.Unmarshal(recBytes, &outputRecord)
	outputBytes, _ := json.Marshal(outputRecord)
	logger.Info("Returning records from getUFADetails " + string(outputBytes))
	return outputBytes, nil
}

//Returns all the UFAs created so far
func getAllUFA(stub shim.ChaincodeStubInterface, who string) ([]byte, error) {
	logger.Info("getAllUFA called")

	recordsList, err := getAllRecordsList(stub)
	if err != nil {
		return nil, errors.New("Unable to get all the records ")
	}
	var outputRecords []map[string]interface{}
	outputRecords = make([]map[string]interface{}, 0)
	for _, ufanumber := range recordsList {
		logger.Info("getAllUFA: Processing record " + ufanumber)
		recBytes, _ := stub.GetState(ufanumber)
		var record map[string]interface{}
		json.Unmarshal(recBytes, &record)
		outputRecords = append(outputRecords, record)
	}
	outputBytes, _ := json.Marshal(outputRecords)
	logger.Info("Returning records from getAllUFA " + string(outputBytes))
	return outputBytes, nil
}

//Returns all the UFA Numbers stored
func getAllRecordsList(stub shim.ChaincodeStubInterface) ([]string, error) {
	var recordList []string
	recBytes, _ := stub.GetState(ALL_ELEMENENTS)

	err := json.Unmarshal(recBytes, &recordList)
	if err != nil {
		return nil, errors.New("Failed to unmarshal getAllRecordsList ")
	}

	return recordList, nil
}

//Validate the new UFA
func validateNewUFAData(args []string) []byte {
	var output string
	msg := validateNewUFA(args[0], args[1])

	if msg == "" {
		output = "{\"validation\":\"Success\",\"msg\" : \"\" }"
	} else {
		output = "{\"validation\":\"Failure\",\"msg\" : \"" + msg + "\" }"
	}
	return []byte(output)
}

// Creating a new Upfront agreement
func createUFA(stub shim.ChaincodeStubInterface, args []string) ([]byte, error) {
	logger.Info("createUFA called")

	ufanumber := args[0]
	who := args[1]
	payload := args[2]
	//If there is no error messages then create the UFA
	valMsg := validateNewUFA(who, payload)
	if valMsg == "" {
		stub.PutState(ufanumber, []byte(payload))

		updateMasterRecords(stub, ufanumber)
		appendUFATransactionHistory(stub, ufanumber, payload)
		logger.Info("Created the UFA after successful validation : " + payload)
	} else {
		return nil, errors.New("Validation failure: " + valMsg)
	}
	return nil, nil
}

//Validate a new UFA
func validateNewUFA(who string, payload string) string {

	//As of now I am checking if who is of proper role
	var validationMessage bytes.Buffer
	var ufaDetails interface{}

	logger.Info("validateNewUFA")
	if who == "SELLER" || who == "BUYER" {
		logger.Info("validateNewUFA WHO")

		json.Unmarshal([]byte(payload), &ufaDetails)
		//Now check individual fields
		ufaRecordMap := ufaDetails.(map[string]interface{})
		netChargeStr := getSafeString(ufaRecordMap["netCharge"])
		tolerenceStr := getSafeString(ufaRecordMap["chargTolrence"])
		netCharge := validateNumber(netChargeStr)
		if netCharge <= 0.0 {
			validationMessage.WriteString("\nInvalid net charge")
		}
		tolerence := validateNumber(tolerenceStr)
		if tolerence < 0.0 || tolerence > 10.0 {
			validationMessage.WriteString("\nTolerence is out of range. Should be between 0 and 10")
		}

	} else {
		validationMessage.WriteString("\nUser is not authorized to create a UFA")
	}
	logger.Info("Validation messagge " + validationMessage.String())
	return validationMessage.String()
}
func getSafeString(input interface{}) string {
	var safeValue string
	var isOk bool

	if input == nil {
		safeValue = ""
	} else {
		safeValue, isOk = input.(string)
		if isOk == false {
			safeValue = ""
		}
	}
	return safeValue
}

//Validate a input string as number or not
func validateNumber(str string) float64 {
	if netCharge, err := strconv.ParseFloat(str, 64); err == nil {
		return netCharge
	}
	return float64(-1.0)
}
func getSafeNumber(input interface{}) float64 {
	return validateNumber(getSafeString(input))
}

//Append to UFA transaction history
func appendUFATransactionHistory(stub shim.ChaincodeStubInterface, ufanumber string, payload string) error {
	var recordList []string

	logger.Info("Appending to transaction history " + ufanumber)
	recBytes, _ := stub.GetState(UFA_TRXN_PREFIX + ufanumber)

	if recBytes == nil {
		logger.Info("Updating the transaction history for the first time")
		recordList = make([]string, 0)
	} else {
		err := json.Unmarshal(recBytes, &recordList)
		if err != nil {
			return errors.New("Failed to unmarshal appendUFATransactionHistory ")
		}
	}
	recordList = append(recordList, payload)
	bytesToStore, _ := json.Marshal(recordList)
	logger.Info("After updating the transaction history" + string(bytesToStore))
	stub.PutState(UFA_TRXN_PREFIX+ufanumber, bytesToStore)
	logger.Info("Appending to transaction history " + ufanumber + " Done!!")
	return nil
}

//Append a new UFA numbetr to the master list
func updateMasterRecords(stub shim.ChaincodeStubInterface, ufaNumber string) error {
	var recordList []string
	recBytes, _ := stub.GetState(ALL_ELEMENENTS)

	err := json.Unmarshal(recBytes, &recordList)
	if err != nil {
		return errors.New("Failed to unmarshal updateMasterReords ")
	}
	recordList = append(recordList, ufaNumber)
	bytesToStore, _ := json.Marshal(recordList)
	logger.Info("After addition" + string(bytesToStore))
	stub.PutState(ALL_ELEMENENTS, bytesToStore)
	return nil
}

//Probe method to check the installation of the chain code in HLF
func probe(stub shim.ChaincodeStubInterface) []byte {
	ts := time.Now().Format(time.UnixDate)
	version, _ := stub.GetState(CHAIN_CODE_VERSION)
	output := "{\"status\":\"Success\",\"ts\" : \"" + ts + "\" ,\"version\" : \"" + string(version) + "\" }"
	return []byte(output)
}

// Init initializes the smart contracts
func (t *UFAChainCode) Init(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {
	logger.Info("Init called")
	//Place an empty arry
	stub.PutState(ALL_ELEMENENTS, []byte("[]"))
	stub.PutState(ALL_INVOICES, []byte("[]"))
	ts := time.Now().Format(time.UnixDate)
	stub.PutState(CHAIN_CODE_VERSION, []byte(ts))
	return nil, nil
}

// Invoke entry point
func (t *UFAChainCode) Invoke(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {
	logger.Info("Invoke called")
	if function == "createUFA" {
		return createUFA(stub, args)
	} else if function == "updateUFA" {
		return updateUFA(stub, args)
	} else if function == "createInvoices" {
		return createInvoices(stub, args)
	} else if function == "updateInvoices" {
		return updateInvoices(stub, args)
	}
	return nil, nil
}

// Query the records form the  smart contracts
func (t *UFAChainCode) Query(stub shim.ChaincodeStubInterface, function string, args []string) ([]byte, error) {
	logger.Info("Query called")
	if function == "probe" {
		return probe(stub), nil
	} else if function == "validateNewUFA" {
		logger.Info("validateNewUFA Going to call")
		return validateNewUFAData(args), nil
	} else if function == "getAllUFA" {
		return getAllUFA(stub, args[0])
	} else if function == "getUFADetails" {
		return getUFADetails(stub, args)
	} else if function == "validateNewInvoideData" {
		return validateNewInvoideData(stub, args), nil
	} else if function == "getInvoicesForUFA" {
		return getInvoicesForUFA(stub, args), nil
	} else if function == "getAllInvoicesForUsr" {
		return getAllInvoicesForUsr(stub, args)
	} else if function == "getAllNonExiredUFA" {
		return getAllNonExpiredUFA(stub, args)
	}
	return nil, nil
}

func main() {
	logger.SetLevel(shim.LogInfo)
	primitives.SetSecurityLevel("SHA3", 256)
	err := shim.Start(new(UFAChainCode))
	if err != nil {
		fmt.Printf("Error starting UFAChainCode: %s", err)
	}
}
