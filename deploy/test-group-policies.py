#!/usr/bin/env python3
"""Validate saved group policies without applying gateway settings."""
import importlib.util,pathlib,unittest
spec=importlib.util.spec_from_file_location('control',pathlib.Path(__file__).with_name('control.py'))
control=importlib.util.module_from_spec(spec);spec.loader.exec_module(control)
class PolicyTests(unittest.TestCase):
 def setUp(self):
  self.config={'routing':{'balancers':[{'tag':'group-7'}]},'outbounds':[{'tag':'auto-vpn-7-1-'},{'tag':'auto-vpn-7-2-'}]}
  self.group={'id':'7','nodes':['1','2'],'interval':30,'mode':'fastest','threshold_ms':1000,'failures':2,'cooldown':60}
 def test_legacy_defaults(self):
  result=control.validate_group_policies(self.config,[self.group])[0]
  self.assertEqual((result['fastest_policy'],result['gain_ms']),('gain',50))
  self.assertEqual(result['return_successes'],20)
 def test_both_comparison_modes_roundtrip(self):
  for kind in ['absolute','gain']:
   group=dict(self.group,fastest_policy=kind,gain_ms=75)
   result=control.validate_group_policies(self.config,[group])[0]
   self.assertEqual((result['fastest_policy'],result['gain_ms']),(kind,75))
 def test_return_policy_roundtrip_and_validation(self):
  group=dict(self.group,mode='failover',return_to_preferred=True,return_successes=20)
  result=control.validate_group_policies(self.config,[group])[0]
  self.assertEqual((result['return_to_preferred'],result['return_successes']),(True,20))
  for fields in [{'return_to_preferred':1},{'return_successes':0},{'return_successes':101},{'return_successes':True}]:
   with self.subTest(fields=fields),self.assertRaises(ValueError):control.validate_group_policies(self.config,[dict(group,**fields)])
 def test_reject_invalid_comparison(self):
  for fields in [{'fastest_policy':'other'},{'gain_ms':True},{'gain_ms':0},{'gain_ms':-1},{'gain_ms':60001}]:
   with self.subTest(fields=fields),self.assertRaises(ValueError):control.validate_group_policies(self.config,[dict(self.group,**fields)])
if __name__=='__main__':unittest.main()
