declare module "node:http" {
  export type IncomingMessage = unknown;
  export type ServerResponse = unknown;
  export type OutgoingHttpHeaders = Record<string, string | number | string[]>;
  export type OutgoingHttpHeader = string | number | string[];
}
