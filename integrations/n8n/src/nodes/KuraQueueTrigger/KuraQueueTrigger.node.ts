import type {
	IDataObject,
	INodeExecutionData,
	INodeType,
	INodeTypeDescription,
	IPollFunctions,
	JsonObject,
} from 'n8n-workflow';
import { LoggerProxy as Logger, NodeApiError, NodeConnectionTypes } from 'n8n-workflow';

const CRED = 'kuraApi';

/**
 * Kura Queue Trigger — polls by claiming release-indexer queue work. It stays
 * idle when no work is claimable and emits one item per claim when work
 * exists. The poll schedule comes from n8n's standard polling UI.
 *
 * Claims are emitted individually because batching is a transport detail of
 * the claim API, not something every consumer should have to unwrap: each
 * item carries its own infohash and claimToken, so a workflow can bind a
 * submission back to the claim it came from without restating identity.
 */
export class KuraQueueTrigger implements INodeType {
	description: INodeTypeDescription = {
		displayName: 'Kura Queue Trigger',
		name: 'kuraQueueTrigger',
		icon: {
			light: 'file:../../assets/kura.svg',
			dark: 'file:../../assets/kura.dark.svg',
		},
		group: ['trigger'],
		version: 3,
		subtitle: '={{"claim: " + $parameter["limit"]}}',
		description: 'Claims Kura release-indexer queue work when releases are available',
		codex: {
			categories: ['Core Nodes'],
			subcategories: {
				'Core Nodes': ['Helpers'],
			},
		},
		defaults: { name: 'Kura Queue Trigger' },
		polling: true,
		inputs: [],
		outputs: [NodeConnectionTypes.Main],
		usableAsTool: true,
		credentials: [{ name: CRED, required: true }],
		properties: [
			{
				displayName: 'Limit',
				name: 'limit',
				type: 'number',
				typeOptions: { minValue: 1 },
				default: 1,
				description: 'Max releases to claim per poll',
			},
			{
				displayName: 'Lease Seconds',
				name: 'leaseSeconds',
				type: 'number',
				default: 300,
				description:
					'Lease length in seconds. Must exceed this workflow worst case from claim through submit; a lease that expires mid-processing makes submit fail with a stale-claim conflict.',
			},
		],
	};

	async poll(this: IPollFunctions): Promise<INodeExecutionData[][] | null> {
		const credentials = await this.getCredentials(CRED);
		const baseUrl = String(credentials.baseUrl).replace(/\/+$/, '');

		let res: IDataObject;
		try {
			res = (await this.helpers.httpRequestWithAuthentication.call(this, CRED, {
				method: 'POST',
				url: `${baseUrl}/api/releases/v1/queue/claim`,
				body: {
					limit: Number(this.getNodeParameter('limit', 10)),
					leaseSeconds: Number(this.getNodeParameter('leaseSeconds', 300)),
				},
				json: true,
			})) as IDataObject;
		} catch (error) {
			Logger.debug('Kura queue trigger claim failed', { err: (error as Error).message });
			throw new NodeApiError(this.getNode(), error as JsonObject);
		}

		const claimed = (res.items as IDataObject[]) ?? [];
		if (claimed.length === 0) {
			Logger.debug('Kura queue trigger poll completed with no claims');
			return null;
		}

		Logger.info('Kura queue trigger emitted claims', { claimed_count: claimed.length });
		return [claimed.map((claim) => ({ json: claim }))];
	}
}
