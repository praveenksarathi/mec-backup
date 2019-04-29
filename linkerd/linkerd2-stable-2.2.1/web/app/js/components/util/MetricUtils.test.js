import deployRollupFixtures from '../../../test/fixtures/deployRollup.json';
import multiDeployRollupFixtures from '../../../test/fixtures/multiDeployRollup.json';
import multiResourceRollupFixtures from '../../../test/fixtures/allRollup.json';
import Percentage from './Percentage';
import {
  processMultiResourceRollup,
  processSingleResourceRollup
} from './MetricUtils.jsx';

describe('MetricUtils', () => {
  describe('processSingleResourceRollup', () => {
    it('Extracts deploy metrics from a single response', () => {
      let result = processSingleResourceRollup(deployRollupFixtures);
      let expectedResult = [
        {
          name: 'voting',
          namespace: 'emojivoto',
          type: 'deployment',
          key: "emojivoto-deployment-voting",
          requestRate: 2.5,
          successRate: 0.9,
          totalRequests: 150,
          tlsRequestPercent: new Percentage(100, 150),
          latency: {
            P50: 1,
            P95: 2,
            P99: 7
          },
          pods: {totalPods: "1", meshedPods: "1", meshedPercentage: new Percentage(1,1)},
          added: true,
          errors: {}
        }
      ];
      expect(result).toHaveLength(1);
      expect(result[0].tlsRequestPercent.prettyRate()).toEqual("66.7%");
      expect(result).toEqual(expectedResult);
    });

    it('Extracts and sorts multiple deploys from a single response', () => {
      let result = processSingleResourceRollup(multiDeployRollupFixtures);
      expect(result).toHaveLength(4);
      expect(result[0].name).toEqual("emoji");
      expect(result[0].namespace).toEqual("emojivoto");
      expect(result[1].name).toEqual("vote-bot");
      expect(result[1].namespace).toEqual("emojivoto");
      expect(result[2].name).toEqual("voting");
      expect(result[2].namespace).toEqual("emojivoto");
      expect(result[3].name).toEqual("web");
      expect(result[3].namespace).toEqual("emojivoto");
    });
  });

  describe('processMultiResourceRollup', () => {
    it('Extracts metrics and groups them by resource type', () => {
      let result = processMultiResourceRollup(multiResourceRollupFixtures);
      expect(Object.keys(result)).toHaveLength(2);

      expect(result["deployment"]).toHaveLength(1);
      expect(result["pod"]).toHaveLength(4);
      expect(result["replicationcontroller"]).toBeUndefined;
    });
  });
});
