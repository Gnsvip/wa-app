package app

import (
	"context"
	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"strings"
)

// Request ordering migrated from pood1e/wa-app@73423e4, retaining this service
// and its persisted registration state. Optional PQ and device signing stay off.
func (e *NativeEngine) existOrderedParams(ctx context.Context, phone *waappv1.PhoneTarget, state nativeState, wamsysCapture *waappv1.WamsysCapture, includeWamsys bool) (orderedParams, error) {
	fields := nativeDeviceMapFields(state)
	lg, lc := registrationLocale(phone)
	params := orderedParams{}
	params.set("cc", phoneCC(phone), false)
	params.set("in", phoneNational(phone), false)
	params.set("lg", lg, false)
	params.set("lc", lc, false)
	params.set("fdid", state.Profile.FDID, false)
	params.set("expid", state.Profile.ExpID, false)
	if state.Profile.AccessSessionID != "" {
		params.set("access_session_id", state.Profile.AccessSessionID, false)
	}
	params.set("id", nativeRegistrationRequestID(state), true)
	params.set("backup_token", state.Profile.BackupToken, true)
	if advertisingID := nativeAdvertisingID(state); advertisingID != "" {
		params.set("advertising_id", advertisingID, false)
	}
	setNonEmptyFormParam(&params, "login", registrationVerificationValue(state, "login"))
	params.set("type", "0", false)
	applyNativeE2EParams(&params, state)

	if token := e.registrationToken(phone, state); token != "" {
		params.set("token", token, false)
	}
	addRawParam(&params, "mistyped", "7")
	addRawParam(&params, "offline_ab", `{"exposure":[],"exp_hash":[],"metrics":{}}`)
	addRawParam(&params, "client_metrics", `{"attempts":1,"app_campaign_download_source":"unknown|unknown","was_activated_from_stub":false}`)
	addRawParam(&params, "read_phone_permission_granted", "0")
	addRawParam(&params, "sim_state", "1")
	addOptionalRawParam(&params, "network_operator_name", fields["network_operator_name"])
	addOptionalRawParam(&params, "sim_operator_name", fields["sim_operator_name"])
	addRawParam(&params, "device_name", normalizeNativePhoneProfile(state.Profile, "").DeviceModel)
	addOptionalRawParam(&params, "backup_token_error", fields["backup_token_error"])
	addOptionalRawParam(&params, "feo2_query_status", fields["feo2_query_status"])
	addRawParam(&params, "is_foa_fdid_app_installed", "false")
	addOptionalRawParam(&params, "device_ram", fields["device_ram"])
	addRawParam(&params, "language_selector_time_spent", "0")
	addRawParam(&params, "language_selector_clicked_count", "0")
	capture, err := e.registrationCapture(ctx, waappv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_EXIST, phone, state, wamsysCapture, includeWamsys)
	if err != nil {
		return nil, err
	}
	if nativeShouldSendRegistrationGPIA(state) {
		applyOrderedWamsysKey(&params, capture, "gpia")
	}
	applyRegistrationADBState(&params, fields, state)
	addOptionalRawParam(&params, "recaptcha", fields["recaptcha"])
	addOptionalRawParam(&params, "network_radio_type", fields["network_radio_type"])
	addOptionalRawParam(&params, "simnum", fields["simnum"])
	addOptionalRawParam(&params, "hasinrc", fields["hasinrc"])
	addOptionalRawParam(&params, "pid", fields["pid"])
	addOptionalRawParam(&params, "rc", fields["rc"])
	addOptionalRawParam(&params, "sim_type", fields["sim_type"])
	addOptionalRawParam(&params, "airplane_mode_type", fields["airplane_mode_type"])
	addOptionalRawParam(&params, "cellular_strength", fields["cellular_strength"])
	addOptionalRawParam(&params, "roaming_type", fields["roaming_type"])
	addOptionalRawParam(&params, "tos_version", fields["tos_version"])
	applyOrderedWamsysExcept(&params, capture, map[string]struct{}{"gpia": {}})
	return params, nil
}

func (e *NativeEngine) registrationCapture(ctx context.Context, kind waappv1.RegistrationRequestKind, phone *waappv1.PhoneTarget, state nativeState, wamsysCapture *waappv1.WamsysCapture, includeWamsys bool) (*waappv1.WamsysCapture, error) {
	if !includeWamsys {
		return nil, nil
	}
	return e.wamsysProvider().RegistrationMaterial(ctx, wamsysMaterialInput{Capture: wamsysCapture, Kind: kind, Phone: phone, State: state, Now: e.clock.Now()})
}

func (e *NativeEngine) codeRequestOrderedParamsWithWamsys(ctx context.Context, phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, state nativeState, authCodeContext string, wamsysCapture *waappv1.WamsysCapture, includeWamsys bool) (orderedParams, error) {
	methodName := registrationMethodName(method, "sms")
	fields := nativeDeviceMapFields(state)
	attempts := nativeCodeRequestAttempts(state)
	lg, lc := registrationLocale(phone)
	params := orderedParams{}
	params.set("cc", phoneCC(phone), false)
	params.set("in", phoneNational(phone), false)
	params.set("lg", lg, false)
	params.set("lc", lc, false)
	params.set("fdid", state.Profile.FDID, false)
	params.set("expid", state.Profile.ExpID, false)
	if state.Profile.AccessSessionID != "" {
		params.set("access_session_id", state.Profile.AccessSessionID, false)
	}
	params.set("id", nativeRegistrationRequestID(state), true)
	params.set("backup_token", state.Profile.BackupToken, true)
	if token := e.registrationToken(phone, state); token != "" {
		params.set("token", token, false)
	}
	params.set("method", methodName, false)
	if nativeRegistrationMethodUsesAuthContext(methodName) {
		if contextValue := strings.TrimSpace(authCodeContext); contextValue != "" {
			params.set("context", contextValue, false)
		}
	}
	if advertisingID := nativeAdvertisingID(state); advertisingID != "" {
		params.set("advertising_id", advertisingID, false)
	}
	setNonEmptyFormParam(&params, "login", registrationVerificationValue(state, "login"))
	params.set("type", "0", false)
	applyNativeE2EParams(&params, state)
	applyNativeCodeRequestMapParams(&params, fields, methodName, attempts, nativeCodeRequestReason(state))

	addOptionalRawParam(&params, "old_phone_number", fields["old_phone_number"])
	addOptionalRawParam(&params, "device_ram", fields["device_ram"])
	capture, err := e.registrationCapture(ctx, waappv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_CODE, phone, state, wamsysCapture, includeWamsys)
	if err != nil {
		return nil, err
	}

	if nativeShouldSendRegistrationGPIA(state) {
		applyOrderedWamsysKey(&params, capture, "gpia")
	}
	applyRegistrationADBState(&params, fields, state)
	applyNativeCodeRequestTailParams(&params, fields)

	applyOrderedWamsysExcept(&params, capture, map[string]struct{}{"gpia": {}})
	addOptionalRawParam(&params, "feo2_query_status", fields["feo2_query_status"])
	return params, nil
}

func applyNativeCodeRequestTailParams(params *orderedParams, fields map[string]string) {
	addOptionalRawParam(params, "recaptcha", fields["recaptcha"])
	addOptionalRawParam(params, "fid", fields["fid"])
	addOptionalRawParam(params, "preloads_app_manager_id", fields["preloads_app_manager_id"])
	addOptionalRawParam(params, "preloads_attribution", fields["preloads_attribution"])
	addOptionalRawParam(params, "tos_version", fields["tos_version"])
	addOptionalRawParam(params, "create_paa", fields["create_paa"])
	addOptionalRawParam(params, "entrypoint", fields["entrypoint"])
	addOptionalRawParam(params, "suma", fields["suma"])
	addOptionalRawParam(params, "cred_token", fields["cred_token"])
}

func (e *NativeEngine) registerOrderedParams(ctx context.Context, phone *waappv1.PhoneTarget, method waappv1.VerificationDeliveryMethod, code string, state nativeState, authCodeContext string, wamsysCapture *waappv1.WamsysCapture, includeWamsys bool) (orderedParams, error) {
	methodName := firstNonEmpty(state.LastCodeParams["method"], registrationMethodName(method, "sms"))
	fields := nativeDeviceMapFields(state)
	lg, lc := registrationLocale(phone)
	params := orderedParams{}
	params.set("cc", phoneCC(phone), false)
	params.set("in", phoneNational(phone), false)
	params.set("lg", firstNonEmpty(state.LastCodeParams["lg"], lg), false)
	params.set("lc", firstNonEmpty(state.LastCodeParams["lc"], lc), false)
	params.set("fdid", firstNonEmpty(state.LastCodeParams["fdid"], state.Profile.FDID), false)
	params.set("expid", firstNonEmpty(state.LastCodeParams["expid"], state.Profile.ExpID), false)
	if value := firstNonEmpty(state.LastCodeParams["access_session_id"], state.Profile.AccessSessionID); value != "" {
		params.set("access_session_id", value, false)
	}
	params.set("id", nativeRegistrationRequestID(state), true)
	params.set("backup_token", firstNonEmpty(state.LastCodeParams["backup_token"], state.Profile.BackupToken), true)
	params.set("code", code, false)
	setNonEmptyFormParam(&params, "auth_response", registrationVerificationValue(state, "auth_response"))
	if nativeRegistrationMethodUsesAuthContext(methodName) {
		if contextValue := firstNonEmpty(registrationVerificationValue(state, "context"), authCodeContext, state.LastCodeParams["context"]); contextValue != "" {
			params.set("context", contextValue, false)
		}
	}
	params.set("method", methodName, false)
	if advertisingID := firstNonEmpty(registrationVerificationValue(state, "advertising_id"), nativeAdvertisingID(state)); advertisingID != "" {
		params.set("advertising_id", advertisingID, false)
	}
	setNonEmptyFormParam(&params, "login", registrationVerificationValue(state, "login"))
	params.set("type", "0", false)
	applyNativeE2EParams(&params, state)
	applyNativeRegisterRequestMapParams(&params, fields, methodName)

	addOptionalRawParam(&params, "old_phone_number", fields["old_phone_number"])
	addOptionalRawParam(&params, "device_ram", fields["device_ram"])
	capture, err := e.registrationCapture(ctx, waappv1.RegistrationRequestKind_REGISTRATION_REQUEST_KIND_REGISTER, phone, state, wamsysCapture, includeWamsys)
	if err != nil {
		return nil, err
	}

	if nativeShouldSendRegistrationGPIA(state) {
		applyOrderedWamsysKey(&params, capture, "gpia")
	}
	applyRegistrationADBState(&params, fields, state)
	applyNativeRegisterRequestTailParams(&params, fields)

	applyOrderedWamsysExcept(&params, capture, map[string]struct{}{"gpia": {}})
	return params, nil
}

func setNonEmptyFormParam(params *orderedParams, key string, value string) {
	if strings.TrimSpace(value) == "" || value == "<redacted>" {
		return
	}
	params.set(key, value, false)
}

func applyNativeRegisterRequestMapParams(params *orderedParams, fields map[string]string, method string) {
	addOptionalRawParam(params, "mistyped", fields["mistyped"])
	addRawParam(params, "client_metrics", nativeRegisterClientMetrics(method))
	addRawParam(params, "entered", nativeCodeEntryMethod(method))
	addOptionalRawParam(params, "mcc", fields["mcc"])
	addOptionalRawParam(params, "mnc", fields["mnc"])
	addOptionalRawParam(params, "sim_mcc", fields["sim_mcc"])
	addOptionalRawParam(params, "sim_mnc", fields["sim_mnc"])
	addOptionalRawParam(params, "network_operator_name", fields["network_operator_name"])
	addOptionalRawParam(params, "sim_operator_name", fields["sim_operator_name"])
	addOptionalRawParam(params, "network_radio_type", fields["network_radio_type"])
	addOptionalRawParam(params, "simnum", fields["simnum"])
	addOptionalRawParam(params, "hasinrc", fields["hasinrc"])
	addOptionalRawParam(params, "pid", fields["pid"])
	addOptionalRawParam(params, "rc", fields["rc"])
	applyNativeCodeRequestSIMSignalParams(params, fields)
}

func applyNativeRegisterRequestTailParams(params *orderedParams, fields map[string]string) {
	addOptionalRawParam(params, "recaptcha", fields["recaptcha"])
	addOptionalRawParam(params, "fid", fields["fid"])
	addOptionalRawParam(params, "preloads_app_manager_id", fields["preloads_app_manager_id"])
	addOptionalRawParam(params, "preloads_attribution", fields["preloads_attribution"])
	addOptionalRawParam(params, "tos_version", fields["tos_version"])
	addOptionalRawParam(params, "cred_token", fields["cred_token"])
	addOptionalRawParam(params, "create_paa", fields["create_paa"])
	addOptionalRawParam(params, "entrypoint", fields["entrypoint"])
	addOptionalRawParam(params, "suma", fields["suma"])
}

func applyRegistrationADBState(params *orderedParams, fields map[string]string, state nativeState) {
	if nativeABBoolFromConfigs(nativePreChatdABConfigs(state), 7999, false) {
		addOptionalRawParam(params, "db", fields["db"])
	}
}
