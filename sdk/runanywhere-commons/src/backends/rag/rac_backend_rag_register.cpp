/**
 * @file rac_backend_rag_register.cpp
 * @brief RAG Backend Registration
 */

#include "rac/backends/rac_rag.h"
#include "rac/core/rac_core.h"
#include "rac/core/rac_logger.h"
#include "rac/features/rag/rac_rag_pipeline.h"

#include <string.h>

#define LOG_TAG "RAG.Register"
#define LOGI(...) RAC_LOG_INFO(LOG_TAG, __VA_ARGS__)
#define LOGE(...) RAC_LOG_ERROR(LOG_TAG, __VA_ARGS__)

// =============================================================================
// MODULE REGISTRATION
// =============================================================================

static const char* MODULE_ID = "rag";
static const char* MODULE_NAME = "RAG Backend";
static const char* MODULE_VERSION = "1.0.0";
static const char* MODULE_DESC = "Retrieval-Augmented Generation with USearch";

extern "C" {

rac_result_t rac_backend_rag_register(void) {
    LOGI("Registering RAG backend module...");

    // RAG doesn't register as a service provider yet; it's a higher-level pipeline using existing services.
    // Pass nullptr for capabilities when num_capabilities is 0 (zero-size array is invalid in C++).
    rac_module_info_t module_info = {
        MODULE_ID,
        MODULE_NAME,
        MODULE_VERSION,
        MODULE_DESC,
        nullptr,  // capabilities
        0         // num_capabilities
    };

    rac_result_t result = rac_module_register(&module_info);
    if (result != RAC_SUCCESS) {
        LOGE("Failed to register RAG module");
        return result;
    }

    LOGI("RAG backend registered successfully");
    return RAC_SUCCESS;
}

rac_result_t rac_backend_rag_unregister(void) {
    LOGI("Unregistering RAG backend...");

    rac_result_t result = rac_module_unregister(MODULE_ID);
    if (result != RAC_SUCCESS) {
        LOGE("Failed to unregister RAG module");
        return result;
    }

    LOGI("RAG backend unregistered");
    return RAC_SUCCESS;
}

} // extern "C"
