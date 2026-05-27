SwaggerUIBundle({
  url: "/api/openapi.yaml",
  dom_id: '#swagger-ui',
  presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
  layout: "BaseLayout",
  deepLinking: true,
  displayRequestDuration: true,
  tryItOutEnabled: true,
});
