import WebSocket from "ws";
import dotenv from "dotenv";

dotenv.config();

export function registerInboundRoutes(fastify) {
  const { ELEVENLABS_API_KEY, ELEVENLABS_AGENT_ID, N8N_AUTH_TOKEN } =
    process.env;

  if (!ELEVENLABS_API_KEY || !ELEVENLABS_AGENT_ID || !N8N_AUTH_TOKEN) {
    throw new Error("Missing required environment variables");
  }

  // Track conversation IDs for inbound calls
  const conversations = new Map();

  class ConversationManager {
    constructor(streamSid) {
      this.streamSid = streamSid;
      this.conversationId = null;
      this.callerPhone = null;
    }
  }

  async function checkUserExists(phone) {
    try {
      const response = await fetch(
        // "http://app-n8n-1:5678/webhook/check-user",
        "https://hackathon.n8n.claimsio.com/webhook/check-user",
        {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
          },
          body: JSON.stringify({ phone }),
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
        `https://api.elevenlabs.io/v1/convai/conversation/get_signed_url?agent_id=...`,
        {
          headers: {
            "xi-api-key": "...",
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

  fastify.all("/incoming-call-eleven", async (request, reply) => {
    const callerPhone = request.body.From;
    console.log(`[Twilio] Incoming call from: ${callerPhone}`);

    const { authorized, userData } = await checkUserExists(callerPhone);
    if (!authorized) {
      const twimlResponse = `<?xml version="1.0" encoding="UTF-8"?>
       <Response>
         <Say>Sorry, you are not authorized to make this call.</Say>
         <Hangup />
       </Response>`;
      return reply.type("text/xml").send(twimlResponse);
    }

    const twimlResponse = `<?xml version="1.0" encoding="UTF-8"?>
     <Response>
       <Connect>
         <Stream url="wss://${request.headers.host}/media-stream">
           <Parameter name="caller_phone" value="${callerPhone}" />
           <Parameter name="user_data" value="${encodeURIComponent(JSON.stringify(userData))}" />
         </Stream>
       </Connect>
     </Response>`;

    reply.type("text/xml").send(twimlResponse);
  });

  fastify.register(async (fastifyInstance) => {
    fastifyInstance.get(
      "/media-stream",
      { websocket: true },
      (connection, req) => {
        console.info("[Server] Twilio connected to media stream");

        let streamSid = null;
        let elevenLabsWs = null;
        let callerPhone = "Unknown";
        let userData = null;
        let conversationManager = null;
        let isDisconnecting = false;

        async function sendWebhook() {
          const manager = conversations.get(streamSid);
          if (manager?.conversationId) {
            const payload = {
              conversation_id: manager.conversationId,
              phone_number: manager.callerPhone,
              call_sid: streamSid,
            };

            console.log("[Webhook] Sending payload:", payload);

            try {
              const response = await fetch(
                "https://hackathon.n8n.claimsio.com/webhook/inbound-calls",
                {
                  method: "POST",
                  headers: {
                    "Content-Type": "application/json",
                    Authorization: `Bearer ${N8N_AUTH_TOKEN}`,
                  },
                  body: JSON.stringify(payload),
                },
              );
              console.log("[Webhook] Response status:", response.status);
              if (!response.ok) {
                const errorText = await response.text();
                console.error("[Webhook] Error response:", errorText);
              }
            } catch (error) {
              console.error("[Webhook] Error:", error);
            }

            conversations.delete(streamSid);
          }
        }



        async function disconnectTwilioCall() {
          if (isDisconnecting) return;

          isDisconnecting = true;
          console.log("[Twilio] Initiating call disconnect");

          // First send the webhook
          await sendWebhook();

          // Then handle Twilio disconnect
          connection.send(
            JSON.stringify({
              event: "mark_done",
              streamSid,
            }),
          );

          connection.send(
            JSON.stringify({
              event: "clear",
              streamSid,
            }),
          );

          connection.send(
            JSON.stringify({
              event: "twiml",
              streamSid,
              twiml: "<Response><Hangup/></Response>",
            }),
          );

          setTimeout(() => {
            if (connection.readyState === WebSocket.OPEN) {
              connection.close();
            }
          }, 1000);
        }

        connection.on("message", async (message) => {
          try {
            const data = JSON.parse(message.toString());
            console.log(`[Twilio] Handling event: ${data.event}`);

            // If we're disconnecting, only process stop events
            if (isDisconnecting && data.event !== "stop") {
              console.log(
                "[Twilio] Ignoring event during disconnect:",
                data.event,
              );
              return;
            }

            if (data.event === "start") {
              streamSid = data.start.streamSid;
              callerPhone =
                data.start.customParameters?.caller_phone || "Unknown";
              conversationManager = new ConversationManager(streamSid);
              conversationManager.callerPhone = callerPhone;
              conversations.set(streamSid, conversationManager);

              try {
                userData = JSON.parse(
                  decodeURIComponent(
                    data.start.customParameters?.user_data || "{}",
                  ),
                );
                console.log(
                  `[Twilio] Stream started - SID: ${streamSid}, Phone: ${callerPhone}`,
                );
                console.log(
                  "[Twilio] User data:",
                  JSON.stringify(userData, null, 2),
                );

                const signedUrl = await getSignedUrl();
                elevenLabsWs = new WebSocket(signedUrl);

                elevenLabsWs.on("open", () => {
                  console.log("[ElevenLabs] Connected to Conversational AI");
                  const clientData = {
                    type: "conversation_initiation_client_data",
                    conversation_config_override: {
                      agent: {
                        prompt: {
                          prompt: `You are an inbound call agent. Context about the caller:
                       Name: ${userData.debtor.first_name} ${userData.debtor.last_name}
                       Case Number: ${userData.case.case_number}
                       Debt Amount: ${userData.case.debt_amount} ${userData.case.currency}
                       Caller Phone: ${callerPhone},
                       Case Description: ${userData.case.case_description}
                      AVAILABLE TOOLS
                      1. debtor_panel: Send SMS with URL to debtor panel where all information about debt as well as secure link to make payment is available. Use is always when debtor want to pay
                          USE ELEVENLABS TOOLS TO SEND SMS - it is named debtor_panel

                       Important: Please have in mind that All monetary values are stored as integers representing the smallest currency unit (e.g., 1000 represents 10.00 PLN).`,
                        },
                        first_message: `Hi ${userData.debtor.first_name}! I see you're calling about your ${userData.case.case_number}. How can I help you today?`,
                      },
                    },
                    client_data: {
                      dynamic_variables: {
                        caller_phone: callerPhone,
                        caller_name: `${userData.debtor.first_name} ${userData.debtor.last_name}`,
                        case_number: userData.case.case_number,
                        debt_amount: `${userData.case.debt_amount} ${userData.case.currency}`,
                        case_description: userData.case.case_description,
                      },
                    },
                  };
                  console.log(
                    "[ElevenLabs] Sending config:",
                    JSON.stringify(clientData, null, 2),
                  );
                  elevenLabsWs.send(JSON.stringify(clientData));
                });

                elevenLabsWs.on("message", (data) => {
                  try {
                    const message = JSON.parse(data.toString());
                    console.log(
                      "[ElevenLabs] Received message type:",
                      message.type,
                    );

                    if (
                      message.type === "conversation_initiation_metadata" &&
                      message.conversation_initiation_metadata_event
                        ?.conversation_id
                    ) {
                      conversationManager.conversationId =
                        message.conversation_initiation_metadata_event.conversation_id;
                      console.log(
                        "[ElevenLabs] Stored conversation ID:",
                        conversationManager.conversationId,
                      );
                    }

                    handleElevenLabsMessage(
                      message,
                      connection,
                      elevenLabsWs,
                      streamSid,
                      disconnectTwilioCall,
                    );
                  } catch (error) {
                    console.error("[ElevenLabs] Error parsing message:", error);
                  }
                });

                elevenLabsWs.on("error", (error) => {
                  console.error("[ElevenLabs] WebSocket error details:", error);
                });

                elevenLabsWs.on("close", (code, reason) => {
                  console.log("[ElevenLabs] Disconnected with code:", code);
                  console.log(
                    "[ElevenLabs] Disconnect reason:",
                    reason?.toString() || "No reason provided",
                  );

                  if (code === 1000 || code === 1005) {
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
                console.error("[Server] Error parsing user data:", error);
              }
            }

            if (
              data.event === "media" &&
              elevenLabsWs?.readyState === WebSocket.OPEN &&
              !isDisconnecting
            ) {
              const audioMessage = {
                user_audio_chunk: Buffer.from(
                  data.media.payload,
                  "base64",
                ).toString("base64"),
              };
              elevenLabsWs.send(JSON.stringify(audioMessage));
            }

            if (data.event === "stop") {
              if (elevenLabsWs?.readyState === WebSocket.OPEN) {
                elevenLabsWs.send(JSON.stringify({ type: "end_conversation" }));
                elevenLabsWs.close();
              }

              // Call disconnect which will also send the webhook
              await disconnectTwilioCall();
            }
          } catch (error) {
            console.error("[Twilio] Error processing message:", error);
          }
        });

        connection.on("close", () => {
          console.log("[Twilio] Client disconnected");
          if (elevenLabsWs?.readyState === WebSocket.OPEN) {
            elevenLabsWs.close();
          }
        });
      },
    );
  });
}

function handleElevenLabsMessage(
  message,
  connection,
  elevenLabsWs,
  streamSid,
  disconnectTwilioCall,
) {
  switch (message.type) {
    case "audio":
      if (message.audio_event?.audio_base_64) {
        const audioData = {
          event: "media",
          streamSid,
          media: {
            payload: message.audio_event.audio_base_64,
          },
        };
        connection.send(JSON.stringify(audioData));
      }
      break;

    case "interruption":
      connection.send(JSON.stringify({ event: "clear", streamSid }));
      break;

    case "ping":
      if (message.ping_event?.event_id) {
        const pongResponse = {
          type: "pong",
          event_id: message.ping_event.event_id,
        };
        elevenLabsWs.send(JSON.stringify(pongResponse));
      }
      break;

    case "end_of_conversation":
      console.log("[ElevenLabs] End of conversation received");
      disconnectTwilioCall();
      break;

    case "conversation_initiation_metadata":
      console.log("[ElevenLabs] Processing metadata response");
      break;

    default:
      console.log(`[ElevenLabs] Handling message type: ${message.type}`);
  }
}
