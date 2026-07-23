"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.KuraApi = void 0;
class KuraApi {
    name = 'kuraApi';
    displayName = 'Kura API';
    documentationUrl = 'https://github.com/wyvernzora/kura';
    icon = 'file:kura.svg';
    properties = [
        {
            displayName: 'Base URL',
            name: 'baseUrl',
            type: 'string',
            default: 'http://kura:8080',
            placeholder: 'http://kura:8080',
            required: true,
            description: 'Base URL of the Kura REST service',
        },
        {
            displayName: 'Bearer Token',
            name: 'bearerToken',
            type: 'string',
            typeOptions: { password: true },
            default: '',
            description: 'Optional KURA_TOKEN value sent as an Authorization bearer token',
        },
    ];
    test = {
        request: {
            baseURL: '={{$credentials.baseUrl}}',
            url: '/api/v1/health',
            method: 'GET',
            headers: {
                Authorization: '={{$credentials.bearerToken ? "Bearer " + $credentials.bearerToken : ""}}',
            },
        },
    };
}
exports.KuraApi = KuraApi;
//# sourceMappingURL=KuraApi.credentials.js.map