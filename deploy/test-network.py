import importlib.util
import pathlib
import unittest
from unittest.mock import patch
from types import SimpleNamespace

spec=importlib.util.spec_from_file_location('control',pathlib.Path(__file__).with_name('control.py'))
control=importlib.util.module_from_spec(spec)
spec.loader.exec_module(control)

class NetworkTests(unittest.TestCase):
    def setUp(self):
        self.network=dict(interface='enp2s0',address='10.20.30.10',cidr='10.20.30.0/24',router='10.20.30.1')

    def test_dynamic_firewall(self):
        text=control.nft_text(self.network)
        for value in ('enp2s0','10.20.30.0/24','10.20.30.10'):
            self.assertIn(value,text)
        for value in ('ens18','192.168.1.84','192.168.1.0/24','__INTERFACE__'):
            self.assertNotIn(value,text)

    def test_reject_injection_and_invalid_addresses(self):
        for field,value in [('interface','enp2s0";bad'),('router','10.20.31.1'),('address','10.20.30.255'),('cidr','10.20.30.10/24')]:
            with self.subTest(field=field):
                n=dict(self.network);n[field]=value
                with self.assertRaises(ValueError):control.validate_network(n)

    def test_applied_network_does_not_follow_new_default(self):
        def fake_run(args,**kwargs):
            self.assertEqual(args,['ip','-j','-4','addr','show','dev','enp2s0'])
            return SimpleNamespace(stdout='[{"addr_info":[{"local":"10.20.30.10","prefixlen":24}]}]')
        with patch.object(control,'run',side_effect=fake_run):
            self.assertEqual(control.applied_network({'network':self.network}),self.network)

    def test_absent_address_rejected_before_mutation(self):
        with patch.object(control,'run',return_value=SimpleNamespace(stdout='[]')):
            with self.assertRaises(ValueError):control.validate_network(self.network,True)

if __name__=='__main__': unittest.main()
