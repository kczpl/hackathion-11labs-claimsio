import WebSocket from "ws";
import Twilio from "twilio";

export function registerOutboundRoutes(fastify) {
  const {
    ELEVENLABS_API_KEY,
    ELEVENLABS_AGENT_ID,
    TWILIO_ACCOUNT_SID,
    TWILIO_AUTH_TOKEN,
    TWILIO_PHONE_NUMBER,
    N8N_AUTH_TOKEN,
  } = process.env;

  if (
    !ELEVENLABS_API_KEY ||
    !ELEVENLABS_AGENT_ID ||
    !TWILIO_ACCOUNT_SID ||
    !TWILIO_AUTH_TOKEN ||
    !TWILIO_PHONE_NUMBER ||
    !N8N_AUTH_TOKEN
  ) {
    throw new Error("Missing required environment variables");
  }

  const twilioClient = new Twilio(TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN);
  const transcripts = new Map();

  class TranscriptManager {
    constructor(callSid) {
      this.callSid = callSid;
      this.conversationId = null;
      this.userData = null;
    }
  }

  async function checkUserData(number) {
    try {
      const response = await fetch(
        "https://n8n.claimsio.com/webhook/check_if_user_exist",
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
          },
          body: JSON.stringify({ phone: number }),
        },
      );
      if (response.status === 200) {
        const data = await response.json();
        return { authorized: true, userData: data };
      }
      return { authorized: false, userData: null };
    } catch (error) {
      console.error("[Auth] Error checking user:", error);
      return { authorized: false, userData: null };
    }
  }

  async function getSignedUrl() {
    try {
      const response = await fetch(
        `https://api.elevenlabs.io/v1/convai/conversation/get_signed_url?agent_id=${ELEVENLABS_AGENT_ID}`,
        {
          method: "GET",
          headers: {
            "xi-api-key": ELEVENLABS_API_KEY,
          },
        },
      );

      if (!response.ok) {
        throw new Error(`Failed to get signed URL: ${response.statusText}`);
      }

      const data = await response.json();
      return data.signed_url;
    } catch (error) {
      console.error("Error getting signed URL:", error);
      throw error;
    }
  }

  async function sendWebhook(payload) {
    try {
      console.log("[Webhook] Sending webhook with token:", N8N_AUTH_TOKEN);
      const response = await fetch(
        "https://n8n.claimsio.com/webhook/outbound-calls",
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: N8N_AUTH_TOKEN, // Removed Bearer prefix
          },
          body: JSON.stringify(payload),
        },
      );
      console.log("[Webhook] Response status:", response.status);
      if (!response.ok) {
        const errorText = await response.text();
        console.error("[Webhook] Error response:", errorText);
        throw new Error(`Webhook failed: ${errorText}`);
      }
    } catch (error) {
      console.error("[Webhook] Error:", error);
      throw error; // Re-throw to handle it in the calling function
    }
  }

  fastify.post("/outbound-call", async (request, reply) => {
    const { number, prompt } = request.body;

    if (!number) {
      return reply.code(400).send({ error: "Phone number is required" });
    }

    try {
      const call = await twilioClient.calls.create({
        from: TWILIO_PHONE_NUMBER,
        to: number,
        url: `https://${request.headers.host}/outbound-call-twiml?prompt=${encodeURIComponent(
          prompt,
        )}&number=${encodeURIComponent(number)}`,
      });

      reply.send({
        success: true,
        message: "Call initiated",
        callSid: call.sid,
      });
    } catch (error) {
      console.error("Error initiating outbound call:", error);
      reply.code(500).send({
        success: false,
        error: "Failed to initiate call",
      });
    }
  });

  fastify.all("/outbound-call-twiml", async (request, reply) => {
    const prompt = request.query.prompt || "";
    const number = request.query.number || "";

    const twimlResponse = `<?xml version="1.0" encoding="UTF-8"?>
      <Response>
        <Connect>
          <Stream url="wss://${request.headers.host}/outbound-media-stream">
            <Parameter name="prompt" value="${prompt}" />
            <Parameter name="number" value="${number}" />
          </Stream>
        </Connect>
      </Response>`;

    reply.type("text/xml").send(twimlResponse);
  });

  fastify.register(async (fastifyInstance) => {
    fastifyInstance.get(
      "/outbound-media-stream",
      { websocket: true },
      (ws, req) => {
        console.info("[Server] Twilio connected to outbound media stream");

        let streamSid = null;
        let callSid = null;
        let elevenLabsWs = null;
        let customParameters = null;
        let transcriptManager = null;
        let isDisconnecting = false;

        async function disconnectTwilioCall() {
          if (isDisconnecting) return;

          isDisconnecting = true;
          console.log("[Twilio] Initiating call disconnect");

          if (transcriptManager?.conversationId) {
            try {
              const payload = {
                conversation_id: transcriptManager.conversationId,
                phone_number: customParameters?.number || "unknown",
                call_sid: callSid || "unknown",
              };
              console.log("[Webhook] Preparing to send payload:", payload);
              await sendWebhook(payload);
              console.log("[Webhook] Successfully sent webhook");
            } catch (error) {
              console.error("[Webhook] Failed to send webhook:", error);
              // Continue with disconnect even if webhook fails
            }
          }

          ws.send(
            JSON.stringify({
              event: "mark_done",
              streamSid,
            }),
          );

          ws.send(
            JSON.stringify({
              event: "clear",
              streamSid,
            }),
          );

          ws.send(
            JSON.stringify({
              event: "twiml",
              streamSid,
              twiml: "<Response><Hangup/></Response>",
            }),
          );

          setTimeout(() => {
            if (ws.readyState === WebSocket.OPEN) {
              ws.close();
            }
          }, 1000);
        }

        async function setupElevenLabsWithUserData(userData) {
          try {
            const signedUrl = await getSignedUrl();
            elevenLabsWs = new WebSocket(signedUrl);

            elevenLabsWs.on("open", () => {
              console.log("[ElevenLabs] Connected to Conversational AI");

              const initialConfig = {
                type: "conversation_initiation_client_data",
                conversation_config_override: {
                  agent: {
                    prompt: {
                      prompt: userData
                        ? `You are an outbound call agent. Context about the person you're calling:
Name: ${userData.debtor.first_name} ${userData.debtor.last_name}
Case Number: ${userData.case.case_number}
Debt Amount: ${userData.case.debt_amount} ${userData.case.currency}
Caller Phone: ${customParameters?.number},
Case Description: ${userData.case.case_description}

${customParameters?.prompt || ""}`
                        : customParameters?.prompt ||
                          "You are a customer service representative",
                    },
                    first_message: userData
                      ? `Hi ${userData.debtor.first_name}! I'm calling about your ${userData.case.case_number}. Do you have a moment to talk?`
                      : "Hello, do you have a moment to talk?",
                  },
                },
              };

              if (userData) {
                initialConfig.client_data = {
                  dynamic_variables: {
                    caller_phone: customParameters?.number || "unknown",
                    caller_name: `${userData.debtor.first_name} ${userData.debtor.last_name}`,
                    case_number: userData.case.case_number,
                    debt_amount: `${userData.case.debt_amount} ${userData.case.currency}`,
                    case_description: userData.case.case_description,
                  },
                };
              }

              console.log(
                "[ElevenLabs] Sending initial config:",
                JSON.stringify(initialConfig, null, 2),
              );
              elevenLabsWs.send(JSON.stringify(initialConfig));
            });

            elevenLabsWs.on("message", (data) => {
              try {
                const message = JSON.parse(data);

                switch (message.type) {
                  case "conversation_initiation_metadata":
                    if (
                      message.conversation_initiation_metadata_event
                        ?.conversation_id
                    ) {
                      transcriptManager.conversationId =
                        message.conversation_initiation_metadata_event.conversation_id;
                      console.log(
                        "[ElevenLabs] Stored conversation_id:",
                        transcriptManager.conversationId,
                      );
                    }
                    break;

                  case "audio":
                    if (streamSid && !isDisconnecting) {
                      const audioData = {
                        event: "media",
                        streamSid,
                        media: {
                          payload:
                            message.audio?.chunk ||
                            message.audio_event?.audio_base_64,
                        },
                      };
                      ws.send(JSON.stringify(audioData));
                    }
                    break;

                  case "interruption":
                    if (streamSid) {
                      ws.send(JSON.stringify({ event: "clear", streamSid }));
                    }
                    break;

                  case "ping":
                    if (message.ping_event?.event_id) {
                      elevenLabsWs.send(
                        JSON.stringify({
                          type: "pong",
                          event_id: message.ping_event.event_id,
                        }),
                      );
                    }
                    break;

                  case "end_of_conversation":
                    console.log("[ElevenLabs] End of conversation received");
                    disconnectTwilioCall();
                    break;

                  default:
                    console.log(
                      `[ElevenLabs] Unhandled message type: ${message.type}`,
                    );
                }
              } catch (error) {
                console.error("[ElevenLabs] Error processing message:", error);
              }
            });

            elevenLabsWs.on("error", (error) => {
              console.error("[ElevenLabs] WebSocket error:", error);
            });

            elevenLabsWs.on("close", (code, reason) => {
              console.log("[ElevenLabs] Disconnected with code:", code);
              console.log(
                "[ElevenLabs] Disconnect reason:",
                reason?.toString() || "No reason provided",
              );

              if (code === 1000 || code === 1005 || code === 1006) {
                console.log(
                  "[ElevenLabs] Normal closure detected, ending Twilio call",
                );
                disconnectTwilioCall();
              } else {
                console.log(
                  `[ElevenLabs] Abnormal closure (code: ${code}), handling gracefully`,
                );
              }
            });
          } catch (error) {
            console.error("[ElevenLabs] Setup error:", error);
          }
        }

        ws.on("error", console.error);

        ws.on("message", async (message) => {
          try {
            const msg = JSON.parse(message);
            console.log(`[Twilio] Received event: ${msg.event}`);

            if (isDisconnecting && msg.event !== "stop") {
              console.log(
                "[Twilio] Ignoring event during disconnect:",
                msg.event,
              );
              return;
            }

            switch (msg.event) {
              case "start":
                streamSid = msg.start.streamSid;
                callSid = msg.start.callSid;
                customParameters = msg.start.customParameters;
                transcriptManager = new TranscriptManager(callSid);
                transcripts.set(callSid, transcriptManager);

                console.log(
                  `[Twilio] Stream started - StreamSid: ${streamSid}, CallSid: ${callSid}`,
                );
                console.log("[Twilio] Start parameters:", customParameters);

                // Fetch user data before setting up ElevenLabs
                const { userData } = await checkUserData(
                  customParameters?.number,
                );
                transcriptManager.userData = userData;

                // Setup ElevenLabs with user data
                await setupElevenLabsWithUserData(userData);
                break;

              case "media":
                if (
                  elevenLabsWs?.readyState === WebSocket.OPEN &&
                  !isDisconnecting
                ) {
                  const audioMessage = {
                    user_audio_chunk: Buffer.from(
                      msg.media.payload,
                      "base64",
                    ).toString("base64"),
                  };
                  elevenLabsWs.send(JSON.stringify(audioMessage));
                }
                break;

              case "stop":
                if (elevenLabsWs?.readyState === WebSocket.OPEN) {
                  elevenLabsWs.send(
                    JSON.stringify({ type: "end_conversation" }),
                  );
                  elevenLabsWs.close();
                }
                await disconnectTwilioCall();
                break;

              default:
                console.log(`[Twilio] Unhandled event: ${msg.event}`);
            }
          } catch (error) {
            console.error("[Twilio] Error processing message:", error);
          }
        });

        ws.on("close", () => {
          console.log("[Twilio] Client disconnected");
          if (elevenLabsWs?.readyState === WebSocket.OPEN) {
            elevenLabsWs.close();
          }
        });
      },
    );
  });

  fastify.get("/transcript/:callSid", async (request, reply) => {
    const { callSid } = request.params;
    const transcript = transcripts.get(callSid);

    if (!transcript) {
      return reply.code(404).send({ error: "Transcript not found" });
    }

    reply.send(transcript);
  });
}
