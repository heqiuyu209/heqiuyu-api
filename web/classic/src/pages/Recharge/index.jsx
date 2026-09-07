/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React from 'react';
import { Card, Typography } from '@douyinfe/semi-ui';

const { Title, Paragraph, Text } = Typography;

const Recharge = () => (
  <div className='mt-[60px] px-2 flex justify-center'>
    <Card className='!rounded-2xl shadow-sm border-0 w-full max-w-lg'>
      <div style={{ textAlign: 'center', padding: '2rem 1rem' }}>
        <Title heading={3} style={{ marginBottom: 20 }}>
          代充 GPT
        </Title>
        <Paragraph
          style={{
            fontSize: 15,
            color: 'var(--semi-color-text-1)',
            marginBottom: 20,
          }}
        >
          如有需要请联系管理员
        </Paragraph>
        <div>
          <Text
            style={{
              fontSize: 16,
              fontWeight: 600,
              color: 'var(--semi-color-primary)',
            }}
          >
            QQ：3756686882
          </Text>
        </div>
      </div>
    </Card>
  </div>
);

export default Recharge;
