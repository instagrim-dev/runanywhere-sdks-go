/**
 * @file openai_handler.cpp
 * @brief OpenAI API endpoint handlers implementation
 *
 * Uses Commons tool calling APIs via the translation layer.
 */

#include "openai_handler.h"
#include "json_utils.h"
#include "openai_translation.h"
#ifdef RAC_HAS_LLAMACPP
#include "rac/backends/rac_llm_llamacpp.h"
#endif
#include "rac/core/rac_audio_utils.h"
#include "rac/core/rac_logger.h"
#include "rac/core/rac_types.h"
#include "rac/features/embeddings/rac_embeddings_service.h"
#include "rac/features/embeddings/rac_embeddings_types.h"
#include "rac/features/llm/rac_tool_calling.h"
#include "rac/features/stt/rac_stt_service.h"
#include "rac/features/stt/rac_stt_types.h"
#include "rac/features/tts/rac_tts_service.h"
#include "rac/features/tts/rac_tts_types.h"

#include <chrono>
#include <cstring>
#include <random>
#include <sstream>

namespace rac {
namespace server {

namespace {

// Find the "data" chunk in an in-memory RIFF/WAVE buffer. Returns true and sets
// out_data/out_size if found; otherwise false (caller may pass whole buffer).
bool findWavDataChunk(const void* wavData, size_t wavSize, const void** outData, size_t* outSize) {
    const uint8_t* p = static_cast<const uint8_t*>(wavData);
    if (wavSize < 12 || std::strncmp(reinterpret_cast<const char*>(p), "RIFF", 4) != 0 ||
        std::strncmp(reinterpret_cast<const char*>(p + 8), "WAVE", 4) != 0) {
        return false;
    }
    size_t offset = 12;
    while (offset + 8 <= wavSize) {
        const char* chunkId = reinterpret_cast<const char*>(p + offset);
        uint32_t chunkSize = 0;
        std::memcpy(&chunkSize, p + offset + 4, 4);
        if (std::strncmp(chunkId, "data", 4) == 0) {
            size_t dataLen = static_cast<size_t>(chunkSize);
            if (offset + 8 + dataLen > wavSize) {
                dataLen = wavSize - (offset + 8);
            }
            *outData = p + offset + 8;
            *outSize = dataLen;
            return true;
        }
        size_t skip = static_cast<size_t>(chunkSize);
        if (offset + 8 + skip > wavSize) {
            skip = wavSize - (offset + 8);
        }
        // RIFF chunks are word-aligned (pad to even byte boundary)
        offset += 8 + ((skip + 1) & ~size_t(1));
    }
    return false;
}

// Parse the "fmt " chunk in a RIFF/WAVE buffer. Returns sample rate in Hz, or 0 if not found/invalid.
int32_t getWavSampleRateFromFmt(const void* wavData, size_t wavSize) {
    const uint8_t* p = static_cast<const uint8_t*>(wavData);
    if (wavSize < 24 || std::strncmp(reinterpret_cast<const char*>(p), "RIFF", 4) != 0 ||
        std::strncmp(reinterpret_cast<const char*>(p + 8), "WAVE", 4) != 0) {
        return 0;
    }
    size_t offset = 12;
    while (offset + 8 <= wavSize) {
        const char* chunkId = reinterpret_cast<const char*>(p + offset);
        uint32_t chunkSize = 0;
        std::memcpy(&chunkSize, p + offset + 4, 4);
        if (std::strncmp(chunkId, "fmt ", 4) == 0 && chunkSize >= 16) {
            uint32_t sampleRate = 0;
            std::memcpy(&sampleRate, p + offset + 8 + 4, 4);
            return static_cast<int32_t>(sampleRate);
        }
        size_t skip = static_cast<size_t>(chunkSize);
        if (offset + 8 + skip > wavSize) {
            break;
        }
        offset += 8 + ((skip + 1) & ~size_t(1));
    }
    return 0;
}

// Content-Type for TTS response from rac_audio_format_enum_t.
const char* ttsContentTypeFromFormat(rac_audio_format_enum_t format) {
    switch (format) {
        case RAC_AUDIO_FORMAT_WAV: return "audio/wav";
        case RAC_AUDIO_FORMAT_MP3: return "audio/mpeg";
        case RAC_AUDIO_FORMAT_OPUS: return "audio/opus";
        case RAC_AUDIO_FORMAT_AAC: return "audio/aac";
        case RAC_AUDIO_FORMAT_FLAC: return "audio/flac";
        case RAC_AUDIO_FORMAT_PCM:
        default: return "audio/wav";  // PCM branch encodes to WAV; default fallback
    }
}

// Generate a random ID for requests
std::string generateId(const std::string& prefix) {
    thread_local std::random_device rd;
    thread_local std::mt19937 gen(rd());
    thread_local std::uniform_int_distribution<uint64_t> dis;

    std::ostringstream ss;
    ss << prefix << std::hex << dis(gen);
    return ss.str();
}

// Get current Unix timestamp
int64_t currentTimestamp() {
    return std::chrono::duration_cast<std::chrono::seconds>(
        std::chrono::system_clock::now().time_since_epoch()
    ).count();
}

} // anonymous namespace

OpenAIHandler::OpenAIHandler(rac_handle_t llmHandle, const std::string& modelId,
                             rac_handle_t sttHandle,
                             rac_handle_t ttsHandle,
                             rac_handle_t embeddingsHandle,
                             const std::string& embeddingsModelId)
    : llmHandle_(llmHandle)
    , modelId_(modelId)
    , sttHandle_(sttHandle)
    , ttsHandle_(ttsHandle)
    , embeddingsHandle_(embeddingsHandle)
    , embeddingsModelId_(embeddingsModelId)
{
}

void OpenAIHandler::handleModels(const httplib::Request& /*req*/, httplib::Response& res) {
#ifndef RAC_HAS_LLAMACPP
    sendError(res, 501, "models not available: LlamaCPP backend not built", "not_implemented");
    return;
#else
    rac_openai_models_response_t response = {};
    response.object = "list";

    rac_openai_model_t model = {};
    model.id = modelId_.c_str();
    model.object = "model";
    model.created = currentTimestamp();
    model.owned_by = "runanywhere";

    response.data = &model;
    response.num_data = 1;

    auto jsonResponse = json::serializeModelsResponse(response);

    res.set_content(jsonResponse.dump(), "application/json");
    res.status = 200;
#endif // RAC_HAS_LLAMACPP
}

void OpenAIHandler::handleChatCompletions(const httplib::Request& req, httplib::Response& res) {
#ifndef RAC_HAS_LLAMACPP
    sendError(res, 501, "chat completions not available: LlamaCPP backend not built", "not_implemented");
    return;
#endif
    // Parse request body
    nlohmann::json requestJson;
    try {
        requestJson = nlohmann::json::parse(req.body);
    } catch (const std::exception& e) {
        sendError(res, 400, std::string("Invalid JSON: ") + e.what(), "invalid_request_error");
        return;
    }

    // Check for required fields
    if (!requestJson.contains("messages") || !requestJson["messages"].is_array()) {
        sendError(res, 400, "Missing required field: messages", "invalid_request_error");
        return;
    }

    if (requestJson["messages"].empty()) {
        sendError(res, 400, "messages array cannot be empty", "invalid_request_error");
        return;
    }

    // Check if streaming is requested
    bool stream = false;
    if (requestJson.contains("stream") && requestJson["stream"].is_boolean()) {
        stream = requestJson["stream"].get<bool>();
    }

    if (stream) {
        processStreaming(req, res, requestJson);
    } else {
        processNonStreaming(req, res, requestJson);
    }
}

void OpenAIHandler::handleHealth(const httplib::Request& /*req*/, httplib::Response& res) {
    nlohmann::json response;
    response["status"] = "ok";
    response["model"] = modelId_;

    // Check if LLM is ready
#ifdef RAC_HAS_LLAMACPP
    if (llmHandle_) {
        response["model_loaded"] = rac_llm_llamacpp_is_model_loaded(llmHandle_) != 0;
    } else {
        response["model_loaded"] = false;
    }
#else
    response["model_loaded"] = false;
#endif

    // v2 capabilities
    response["stt_available"] = (sttHandle_ != nullptr);
    response["tts_available"] = (ttsHandle_ != nullptr);
    response["embeddings_available"] = (embeddingsHandle_ != nullptr);

    res.set_content(response.dump(), "application/json");
    res.status = 200;
}

void OpenAIHandler::handleTranscriptions(const httplib::Request& req, httplib::Response& res) {
    if (!sttHandle_) {
        sendError(res, 501, "transcriptions not configured: set STT model path (e.g. --stt-model)", "not_implemented");
        return;
    }
    if (!req.has_file("file")) {
        sendError(res, 400, "missing required field: file (multipart form)", "invalid_request_error");
        return;
    }
    auto file = req.get_file_value("file");
    std::string audio = file.content;
    if (audio.empty()) {
        sendError(res, 400, "file content is empty", "invalid_request_error");
        return;
    }
    // Reject known compressed formats; only WAV (RIFF) or raw PCM is supported.
    if (audio.size() >= 3 && audio.substr(0, 3) == "ID3") {
        sendError(res, 400, "unsupported audio format: MP3 not supported; use WAV (RIFF) or raw PCM", "invalid_request_error");
        return;
    }
    if (audio.size() >= 8 && audio.substr(4, 4) == "ftyp") {
        sendError(res, 400, "unsupported audio format: M4A/MP4 not supported; use WAV (RIFF) or raw PCM", "invalid_request_error");
        return;
    }
    if (audio.size() >= 4 && audio.substr(0, 4) == "OggS") {
        sendError(res, 400, "unsupported audio format: OGG not supported; use WAV (RIFF) or raw PCM", "invalid_request_error");
        return;
    }
    // If upload is RIFF/WAVE, locate the "data" chunk and pass raw PCM only.
    // The STT backend (e.g. ONNX) expects headerless PCM; sample rate is set via options.
    const void* audioData = audio.data();
    size_t audioSize = audio.size();
    int32_t wavSampleRate = 0;
    if (audio.size() >= 12 && audio.substr(0, 4) == "RIFF") {
        wavSampleRate = getWavSampleRateFromFmt(audio.data(), audio.size());
        const void* dataChunk = nullptr;
        size_t dataChunkSize = 0;
        if (findWavDataChunk(audio.data(), audio.size(), &dataChunk, &dataChunkSize)) {
            audioData = dataChunk;
            audioSize = dataChunkSize;
        }
    }
    rac_stt_options_t options = RAC_STT_OPTIONS_DEFAULT;
    if (wavSampleRate > 0) {
        options.sample_rate = wavSampleRate;
    }
    // Language: read from query param (cpp-httplib v0.15.3 does not expose multipart form fields;
    // OpenAI spec uses form field "language" — when upgrading httplib, prefer req.form.get_field("language")).
    std::string langParam = req.get_param_value("language");
    if (!langParam.empty()) {
        options.language = langParam.c_str();
    }
    rac_stt_result_t sttResult = {};
    rac_result_t rc = rac_stt_transcribe(sttHandle_, audioData, audioSize, &options, &sttResult);
    if (RAC_FAILED(rc)) {
        sendError(res, 500, "transcription failed", "server_error");
        return;
    }
    nlohmann::json out;
    out["text"] = sttResult.text ? sttResult.text : "";
    res.set_content(out.dump(), "application/json");
    res.status = 200;
    rac_stt_result_free(&sttResult);
}

void OpenAIHandler::handleSpeech(const httplib::Request& req, httplib::Response& res) {
    if (!ttsHandle_) {
        sendError(res, 501, "speech not configured: set TTS model path (e.g. --tts-model)", "not_implemented");
        return;
    }
    nlohmann::json body;
    try {
        body = nlohmann::json::parse(req.body);
    } catch (const std::exception& e) {
        sendError(res, 400, std::string("Invalid JSON: ") + e.what(), "invalid_request_error");
        return;
    }
    if (!body.contains("input") || !body["input"].is_string()) {
        sendError(res, 400, "missing required field: input (string)", "invalid_request_error");
        return;
    }
    std::string text = body["input"].get<std::string>();
    rac_tts_result_t ttsResult = {};
    rac_result_t rc = rac_tts_synthesize(ttsHandle_, text.c_str(), nullptr, &ttsResult);
    if (RAC_FAILED(rc)) {
        sendError(res, 500, "synthesis failed", "server_error");
        return;
    }
    if (!ttsResult.audio_data || ttsResult.audio_size == 0) {
        rac_tts_result_free(&ttsResult);
        sendError(res, 500, "no audio produced", "server_error");
        return;
    }
    if (ttsResult.audio_format == RAC_AUDIO_FORMAT_PCM) {
        void* wavData = nullptr;
        size_t wavSize = 0;
        rc = rac_audio_float32_to_wav(ttsResult.audio_data, ttsResult.audio_size,
                                      ttsResult.sample_rate > 0 ? ttsResult.sample_rate : 22050,
                                      &wavData, &wavSize);
        rac_tts_result_free(&ttsResult);
        if (RAC_FAILED(rc) || !wavData) {
            sendError(res, 500, "audio conversion failed", "server_error");
            return;
        }
        res.set_content(std::string(static_cast<const char*>(wavData), wavSize), "audio/wav");
        rac_free(wavData);
    } else {
        const char* contentType = ttsContentTypeFromFormat(ttsResult.audio_format);
        res.set_content(std::string(static_cast<const char*>(ttsResult.audio_data),
                                    ttsResult.audio_size),
                       contentType);
        rac_tts_result_free(&ttsResult);
    }
    res.status = 200;
}

void OpenAIHandler::handleEmbeddings(const httplib::Request& req, httplib::Response& res) {
    if (!embeddingsHandle_) {
        sendError(res, 501, "embeddings not configured: set embeddings model path (e.g. --embeddings-model)", "not_implemented");
        return;
    }
    nlohmann::json body;
    try {
        body = nlohmann::json::parse(req.body);
    } catch (const std::exception& e) {
        sendError(res, 400, std::string("Invalid JSON: ") + e.what(), "invalid_request_error");
        return;
    }
    if (!body.contains("input")) {
        sendError(res, 400, "missing required field: input", "invalid_request_error");
        return;
    }
    // Validate "model" before calling embed so we do not leak embResult on 400.
    if (body.contains("model") && !body["model"].is_string()) {
        sendError(res, 400, "model must be a string", "invalid_request_error");
        return;
    }
    rac_embeddings_result_t embResult = {};
    rac_result_t rc;
    if (body["input"].is_array()) {
        std::vector<std::string> texts;
        for (const auto& el : body["input"]) {
            if (!el.is_string()) {
                sendError(res, 400, "input array must contain only strings", "invalid_request_error");
                return;
            }
            texts.push_back(el.get<std::string>());
        }
        if (texts.empty()) {
            sendError(res, 400, "input array must contain at least one string", "invalid_request_error");
            return;
        }
        std::vector<const char*> ptrs;
        for (const auto& s : texts) {
            ptrs.push_back(s.c_str());
        }
        rc = rac_embeddings_embed_batch(embeddingsHandle_, ptrs.data(), ptrs.size(),
                                        nullptr, &embResult);
        if (RAC_FAILED(rc)) {
            // Fallback: embed each item separately (batch backend may be unavailable)
            std::string embedModel = embeddingsModelId_;
            if (body.contains("model")) {
                embedModel = body["model"].get<std::string>();
            }
            nlohmann::json out;
            out["object"] = "list";
            out["model"] = embedModel;
            out["data"] = nlohmann::json::array();
            int32_t totalTokens = 0;
            for (size_t i = 0; i < texts.size(); ++i) {
                rac_embeddings_result_t singleResult = {};
                rc = rac_embeddings_embed(embeddingsHandle_, texts[i].c_str(), nullptr, &singleResult);
                if (RAC_FAILED(rc)) {
                    rac_embeddings_result_free(&singleResult);
                    sendError(res, 500, "embeddings failed", "server_error");
                    return;
                }
                nlohmann::json obj;
                obj["object"] = "embedding";
                obj["index"] = static_cast<int>(i);
                if (singleResult.embeddings && singleResult.num_embeddings > 0 && singleResult.embeddings[0].data) {
                    std::vector<float> vec(singleResult.embeddings[0].data,
                                          singleResult.embeddings[0].data + singleResult.embeddings[0].dimension);
                    obj["embedding"] = vec;
                } else {
                    obj["embedding"] = nlohmann::json::array();
                }
                out["data"].push_back(obj);
                totalTokens += singleResult.total_tokens;
                rac_embeddings_result_free(&singleResult);
            }
            out["usage"] = nlohmann::json::object({
                {"prompt_tokens", totalTokens},
                {"total_tokens", totalTokens}
            });
            res.set_content(out.dump(), "application/json");
            res.status = 200;
            return;
        }
    } else if (body["input"].is_string()) {
        std::string text = body["input"].get<std::string>();
        rc = rac_embeddings_embed(embeddingsHandle_, text.c_str(), nullptr, &embResult);
    } else {
        sendError(res, 400, "input must be string or array of strings", "invalid_request_error");
        return;
    }
    if (RAC_FAILED(rc)) {
        sendError(res, 500, "embeddings failed", "server_error");
        return;
    }
    std::string embedModel = embeddingsModelId_;
    if (body.contains("model")) {
        embedModel = body["model"].get<std::string>();
    }
    nlohmann::json out;
    out["object"] = "list";
    out["model"] = embedModel;
    out["data"] = nlohmann::json::array();
    int32_t totalTokens = 0;
    if (embResult.embeddings) {
        for (size_t i = 0; i < embResult.num_embeddings; ++i) {
            nlohmann::json obj;
            obj["object"] = "embedding";
            obj["index"] = static_cast<int>(i);
            if (embResult.embeddings[i].data) {
                std::vector<float> vec(embResult.embeddings[i].data,
                                       embResult.embeddings[i].data + embResult.embeddings[i].dimension);
                obj["embedding"] = vec;
            } else {
                obj["embedding"] = nlohmann::json::array();
            }
            out["data"].push_back(obj);
        }
        totalTokens = embResult.total_tokens;
    }
    out["usage"] = nlohmann::json::object({
        {"prompt_tokens", totalTokens},
        {"total_tokens", totalTokens}
    });
    res.set_content(out.dump(), "application/json");
    res.status = 200;
    rac_embeddings_result_free(&embResult);
}

void OpenAIHandler::processNonStreaming(const httplib::Request& /*req*/,
                                         httplib::Response& res,
                                         const nlohmann::json& requestJson) {
#ifndef RAC_HAS_LLAMACPP
    sendError(res, 501, "chat completions not available: LlamaCPP backend not built", "not_implemented");
    return;
#else
    RAC_LOG_INFO("Server", "processNonStreaming: START");

    // Get messages and tools from request
    const auto& messages = requestJson["messages"];
    nlohmann::json tools = requestJson.value("tools", nlohmann::json::array());
    RAC_LOG_INFO("Server", "processNonStreaming: messages count=%zu, tools count=%zu",
                 messages.size(), tools.size());

    // Build prompt using translation layer (which uses Commons APIs)
    RAC_LOG_INFO("Server", "processNonStreaming: building prompt...");
    std::string prompt = translation::buildPromptFromOpenAI(messages, tools, nullptr);
    RAC_LOG_INFO("Server", "processNonStreaming: prompt built, length=%zu", prompt.length());

    // DEBUG: Log the messages JSON and built prompt
    RAC_LOG_DEBUG("Server", "=== REQUEST MESSAGES JSON ===");
    RAC_LOG_DEBUG("Server", "%s", messages.dump(2).c_str());
    RAC_LOG_DEBUG("Server", "=== BUILT PROMPT (first 2000 chars) ===");
    RAC_LOG_DEBUG("Server", "%s", prompt.substr(0, 2000).c_str());
    RAC_LOG_DEBUG("Server", "=== END PROMPT ===");

    // Parse LLM options
    rac_llm_options_t options = parseOptions(requestJson);
    RAC_LOG_INFO("Server", "processNonStreaming: options parsed, max_tokens=%d, temp=%.2f",
                 options.max_tokens, options.temperature);

    // Generate response using LlamaCPP backend directly
    RAC_LOG_INFO("Server", "processNonStreaming: calling rac_llm_llamacpp_generate with handle=%p", (void*)llmHandle_);
    rac_llm_result_t result = {};
    rac_result_t rc = rac_llm_llamacpp_generate(llmHandle_, prompt.c_str(), &options, &result);
    RAC_LOG_INFO("Server", "processNonStreaming: rac_llm_llamacpp_generate returned rc=%d", rc);

    if (RAC_FAILED(rc)) {
        sendError(res, 500, "Generation failed", "server_error");
        return;
    }

    // Update token count
    totalTokensGenerated_ += result.completion_tokens;

    // Check if the response contains a tool call using Commons API
    rac_tool_call_t toolCall = {};
    bool hasToolCall = false;

    if (result.text && !tools.empty()) {
        rac_result_t parseResult = rac_tool_call_parse(result.text, &toolCall);
        hasToolCall = (parseResult == RAC_SUCCESS && toolCall.has_tool_call);
    }

    // Build response
    std::string requestId = generateId("chatcmpl-");

    rac_openai_chat_response_t response = {};
    response.id = const_cast<char*>(requestId.c_str());
    response.object = "chat.completion";
    response.created = currentTimestamp();
    response.model = modelId_.c_str();

    // Create message with potential tool calls
    rac_openai_assistant_message_t message = {};
    message.role = RAC_OPENAI_ROLE_ASSISTANT;

    // Tool call storage (for lifetime management)
    rac_openai_tool_call_t openaiToolCall = {};
    std::string toolCallId;
    std::string toolName;
    std::string toolArgs;

    if (hasToolCall) {
        // Convert Commons tool call to OpenAI format
        toolCallId = translation::generateToolCallId();
        toolName = toolCall.tool_name ? toolCall.tool_name : "";
        toolArgs = toolCall.arguments_json ? toolCall.arguments_json : "{}";

        openaiToolCall.id = toolCallId.c_str();
        openaiToolCall.type = "function";
        openaiToolCall.function_name = toolName.c_str();
        openaiToolCall.function_arguments = toolArgs.c_str();

        message.content = toolCall.clean_text; // Text without tool call tags
        message.tool_calls = &openaiToolCall;
        message.num_tool_calls = 1;
    } else {
        message.content = result.text;
        message.tool_calls = nullptr;
        message.num_tool_calls = 0;
    }

    rac_openai_choice_t choice = {};
    choice.index = 0;
    choice.message = message;
    choice.finish_reason = hasToolCall ? RAC_OPENAI_FINISH_TOOL_CALLS : RAC_OPENAI_FINISH_STOP;

    response.choices = &choice;
    response.num_choices = 1;

    response.usage.prompt_tokens = result.prompt_tokens;
    response.usage.completion_tokens = result.completion_tokens;
    response.usage.total_tokens = result.total_tokens;

    auto jsonResponse = json::serializeChatResponse(response);

    // Clean up
    rac_llm_result_free(&result);
    if (hasToolCall) {
        rac_tool_call_free(&toolCall);
    }

    res.set_content(jsonResponse.dump(), "application/json");
    res.status = 200;
#endif // RAC_HAS_LLAMACPP
}

void OpenAIHandler::processStreaming(const httplib::Request& /*req*/,
                                      httplib::Response& res,
                                      const nlohmann::json& requestJson) {
#ifndef RAC_HAS_LLAMACPP
    sendError(res, 501, "chat completions not available: LlamaCPP backend not built", "not_implemented");
    return;
#else
    // Get messages and tools from request
    const auto& messages = requestJson["messages"];
    nlohmann::json tools = requestJson.value("tools", nlohmann::json::array());

    // Build prompt using translation layer
    std::string prompt = translation::buildPromptFromOpenAI(messages, tools, nullptr);

    // Parse options
    rac_llm_options_t options = parseOptions(requestJson);
    options.streaming_enabled = RAC_TRUE;

    // Generate request ID
    std::string requestId = generateId("chatcmpl-");
    int64_t created = currentTimestamp();

    // Set up streaming response
    res.set_header("Content-Type", "text/event-stream");
    res.set_header("Cache-Control", "no-cache");
    res.set_header("Connection", "keep-alive");

    // Start streaming via content provider
    res.set_content_provider(
        "text/event-stream",
        [this, prompt, options, requestId, created](size_t /*offset*/, httplib::DataSink& sink) mutable {
            // First chunk: send role
            {
                rac_openai_stream_chunk_t chunk = {};
                chunk.id = requestId.c_str();
                chunk.object = "chat.completion.chunk";
                chunk.created = created;
                chunk.model = modelId_.c_str();

                rac_openai_delta_t delta = {};
                delta.role = "assistant";
                delta.content = nullptr;

                rac_openai_stream_choice_t choice = {};
                choice.index = 0;
                choice.delta = delta;
                choice.finish_reason = RAC_OPENAI_FINISH_NONE;

                chunk.choices = &choice;
                chunk.num_choices = 1;

                std::string sseData = json::formatSSE(json::serializeStreamChunk(chunk));
                sink.write(sseData.c_str(), sseData.size());
            }

            // Stream tokens incrementally via rac_llm_llamacpp_generate_stream
            struct StreamCtx {
                httplib::DataSink* sink;
                const std::string* requestId;
                const std::string* modelId;
                int64_t created;
                int32_t tokenCount;
            };

            StreamCtx ctx = { &sink, &requestId, &modelId_, created, 0 };

            auto streamCallback = [](const char* token, rac_bool_t is_final, void* user_data) -> rac_bool_t {
                auto* ctx = static_cast<StreamCtx*>(user_data);

                if (is_final) {
                    // Send finish chunk
                    rac_openai_stream_chunk_t chunk = {};
                    chunk.id = ctx->requestId->c_str();
                    chunk.object = "chat.completion.chunk";
                    chunk.created = ctx->created;
                    chunk.model = ctx->modelId->c_str();

                    rac_openai_delta_t delta = {};
                    delta.role = nullptr;
                    delta.content = nullptr;

                    rac_openai_stream_choice_t choice = {};
                    choice.index = 0;
                    choice.delta = delta;
                    choice.finish_reason = RAC_OPENAI_FINISH_STOP;

                    chunk.choices = &choice;
                    chunk.num_choices = 1;

                    std::string sseData = json::formatSSE(json::serializeStreamChunk(chunk));
                    ctx->sink->write(sseData.c_str(), sseData.size());
                } else if (token && token[0] != '\0') {
                    // Send content chunk with this token
                    rac_openai_stream_chunk_t chunk = {};
                    chunk.id = ctx->requestId->c_str();
                    chunk.object = "chat.completion.chunk";
                    chunk.created = ctx->created;
                    chunk.model = ctx->modelId->c_str();

                    rac_openai_delta_t delta = {};
                    delta.role = nullptr;
                    delta.content = token;

                    rac_openai_stream_choice_t choice = {};
                    choice.index = 0;
                    choice.delta = delta;
                    choice.finish_reason = RAC_OPENAI_FINISH_NONE;

                    chunk.choices = &choice;
                    chunk.num_choices = 1;

                    std::string sseData = json::formatSSE(json::serializeStreamChunk(chunk));
                    ctx->sink->write(sseData.c_str(), sseData.size());
                    ctx->tokenCount++;
                }

                return RAC_TRUE;  // Continue generating
            };

            rac_result_t rc = rac_llm_llamacpp_generate_stream(
                llmHandle_, prompt.c_str(), &options, streamCallback, &ctx);

            if (RAC_FAILED(rc)) {
                RAC_LOG_ERROR("Server", "Streaming generation failed: %d", rc);
            }

            totalTokensGenerated_ += ctx.tokenCount;

            // Send [DONE]
            std::string doneData = json::formatSSEDone();
            sink.write(doneData.c_str(), doneData.size());

            sink.done();
            return true;
        }
    );

    res.status = 200;
#endif // RAC_HAS_LLAMACPP
}

rac_llm_options_t OpenAIHandler::parseOptions(const nlohmann::json& requestJson) {
    rac_llm_options_t options = RAC_LLM_OPTIONS_DEFAULT;

    if (requestJson.contains("temperature") && requestJson["temperature"].is_number()) {
        options.temperature = requestJson["temperature"].get<float>();
    }

    if (requestJson.contains("top_p") && requestJson["top_p"].is_number()) {
        options.top_p = requestJson["top_p"].get<float>();
    }

    if (requestJson.contains("max_tokens") && requestJson["max_tokens"].is_number()) {
        options.max_tokens = requestJson["max_tokens"].get<int32_t>();
    }

    return options;
}

void OpenAIHandler::sendError(httplib::Response& res, int statusCode,
                               const std::string& message, const std::string& type) {
    auto errorJson = json::createErrorResponse(message, type, statusCode);
    res.set_content(errorJson.dump(), "application/json");
    res.status = statusCode;
}

} // namespace server
} // namespace rac
